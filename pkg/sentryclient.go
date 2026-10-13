// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
)

//counterfeiter:generate -o ../mocks/sentry_client.go --fake-name SentryClient . SentryClient

// IssueRef is the Sentry-side identity of one event: the internal numeric issue
// id plus its canonical issue URL. URL construction lives here so the consumer
// stays dumb about Sentry's URL shape.
type IssueRef struct {
	NumericID string
	URL       string
}

// ErrEventNotFound reports that Sentry holds no record of the event. It is
// control flow, not a failure: a forwarded event dropped by quota is exactly
// the case the derived-key path exists for, so the caller keeps its
// envelope-derived identity without counting an error.
var ErrEventNotFound = errors.New(context.Background(), "event not found in sentry")

// ErrRetriesExhausted reports that every retryable attempt at one lookup
// failed. It exists so the consumer can tell "we tried and gave up" apart from
// a single non-retryable failure: the two mean different things in the metric
// (a sustained rate limit versus a one-off), and only the former is worth
// alerting on.
var ErrRetriesExhausted = errors.New(context.Background(), "sentry lookup retries exhausted")

// SentryClient resolves the Sentry-side identity of one event id. The consumer
// enriches only forwarded events: a rejected event was never forwarded to
// Sentry, so its lookup is a guaranteed 404 that burns API quota on a
// quota-limited org where rejected is the majority of traffic.
type SentryClient interface {
	// IssueRef resolves eventID to its Sentry issue. Returns ErrEventNotFound
	// when Sentry holds no record of the event.
	//
	// eventTimestamp is the event's own creation time, and it gates the
	// dedicated 404 schedule: a 404 inside Sentry's ingest-indexing window is
	// transient, but the same 404 on an event already past that window is a
	// real miss and must not cost the schedule's wait. A zero value means
	// "age unknown" and is treated as old — the schedule is skipped rather
	// than holding a Kafka record for 10 s on an age we cannot establish.
	IssueRef(ctx context.Context, eventID string, eventTimestamp time.Time) (IssueRef, error)
}

// sentryClientTimeout bounds one lookup so a slow Sentry cannot stall the
// Kafka consume loop indefinitely.
const sentryClientTimeout = 10 * time.Second

// Retry bounds. Sentry rate-limits in bursts (three 429s ~230 ms apart,
// observed on dev 2026-09-14), so the failure this retries against is short —
// a couple of hundred milliseconds of backoff clears it. The budget is
// deliberately far below sentryClientTimeout: enrichment is best-effort, and a
// task waiting on retries is a task not yet published.
const (
	// sentryMaxAttempts counts the first try, so this is 1 attempt + 2 retries.
	sentryMaxAttempts = 3
	// sentryRetryBudget caps total time spent *waiting* across one lookup.
	// A Retry-After longer than the remaining budget ends the loop rather than
	// honouring it — Sentry asking for 60 s is a "come back later", not a
	// reason to hold a Kafka record.
	sentryRetryBudget = 2 * time.Second
	// sentryBaseBackoff is the first wait; each retry doubles it (100ms, 200ms).
	sentryBaseBackoff = 100 * time.Millisecond
)

// sentry404Schedule is the dedicated 404 retry schedule: absolute offsets from
// the first attempt, so the gaps are 1 s, 2 s, 3 s, 4 s — 10 s of waiting and
// 4 extra lookups.
//
// It exists because a 404 from the eventids endpoint is ambiguous. Sentry
// returns it both for an event it never stored and for one it has stored but
// not yet indexed. Two reproduced misses landed 2.58 s and 0.91 s after the
// event's date_created and resolved on a later lookup, so the second class is
// real — and the error-retry budget above (≈300 ms total) is two orders of
// magnitude too short to clear it.
//
// The bound is deliberately the same order as the per-lookup
// sentryClientTimeout already accepted on the consume loop.
var sentry404Schedule = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	6 * time.Second,
	10 * time.Second,
}

// sentry404MaxEventAge bounds the schedule to events that could still be inside
// Sentry's indexing window. An event older than this was replayed from the
// topic rather than freshly forwarded, so its 404 is a real miss: retrying it
// would hold a partition for 10 s per record, and a replay burst of 45 misses
// would stall the consumer for roughly 7.5 minutes.
const sentry404MaxEventAge = 30 * time.Second

// NewSentryClient returns the HTTP SentryClient for the documented eventids
// endpoint ({url}/api/0/organizations/{org}/eventids/{event_id}/, scope
// org:read). Constructor-injected: no package-level state, and the token lives
// only in memory.
func NewSentryClient(url, org, token string) SentryClient {
	return NewSentryClientWithWaiter(url, org, token, libtime.NewWaiterDuration())
}

// NewSentryClientWithWaiter is NewSentryClient with the backoff waiter
// injected, so tests drive the retry schedule without sleeping in real time.
func NewSentryClientWithWaiter(url, org, token string, waiter libtime.WaiterDuration) SentryClient {
	return &sentryClient{
		baseURL: strings.TrimSuffix(url, "/"),
		org:     org,
		token:   token,
		client:  &http.Client{Timeout: sentryClientTimeout},
		waiter:  waiter,
	}
}

type sentryClient struct {
	baseURL string
	org     string
	token   string
	client  *http.Client
	waiter  libtime.WaiterDuration
}

// eventIDResponse is the documented 200 body of the eventids endpoint: the
// event's internal group (issue) id, with the project slug alongside it.
type eventIDResponse struct {
	Event struct {
		GroupID string `json:"groupID"`
	} `json:"event"`
	ProjectSlug string `json:"projectSlug"`
}

// IssueRef implements SentryClient: one bounded error-retry pass, then — only
// when the 404 came back for an event young enough that Sentry may still be
// indexing it — the dedicated 404 schedule.
//
// The two budgets are deliberately separate. The error-retry budget answers
// "is Sentry having a moment?"; the 404 schedule answers "has Sentry finished
// indexing this event yet?". A 404 that arrives for an event past the indexing
// window is a real miss and costs exactly the one call already made.
func (c *sentryClient) IssueRef(
	ctx context.Context,
	eventID string,
	eventTimestamp time.Time,
) (IssueRef, error) {
	ref, err := c.issueRefWithErrorRetry(ctx, eventID)
	if !errors.Is(err, ErrEventNotFound) {
		return ref, err
	}
	if time.Since(eventTimestamp) > sentry404MaxEventAge {
		return IssueRef{}, err
	}
	return c.issueRefWith404Schedule(ctx, eventID)
}

// issueRefWith404Schedule re-tries a 404 at the fixed offsets in
// sentry404Schedule. Only a 404 continues the schedule: a success ends it, and
// so does any other outcome, because the error-retry budget has already been
// spent on the first attempt and the point here is to outlast Sentry's
// indexing lag rather than to retry the world. A 429 met during the schedule
// therefore surfaces as ErrRetriesExhausted, the same classification the
// caller would have seen without the schedule.
func (c *sentryClient) issueRefWith404Schedule(
	ctx context.Context,
	eventID string,
) (IssueRef, error) {
	var previous time.Duration
	for i, offset := range sentry404Schedule {
		wait := offset - previous
		previous = offset
		if waitErr := c.waiter.Wait(ctx, libtime.Duration(wait)); waitErr != nil {
			// Context cancelled mid-schedule — the consume loop is shutting
			// down, so surface that rather than the Sentry outcome.
			return IssueRef{}, errors.Wrap(ctx, waitErr, "wait before sentry 404 retry")
		}
		glog.V(2).Infof(
			"404, retrying eventID=%s attempt=%d offset=%v", eventID, i+2, offset,
		)
		ref, err := c.issueRefWithErrorRetry(ctx, eventID)
		if err == nil {
			return ref, nil
		}
		if !errors.Is(err, ErrEventNotFound) {
			return IssueRef{}, err
		}
	}
	return IssueRef{}, ErrEventNotFound
}

// issueRefWithErrorRetry performs the bounded retry against the transient
// failure classes.
//
// Retryable: 429, 502, 503, 504, and transport errors. NOT retryable: 404
// (ErrEventNotFound — control flow, handled by IssueRef's own schedule) and a
// bare 500. Some APIs return 500 for a missing resource, so retrying it burns
// quota on a permanent miss; if Sentry is genuinely broken it says so with a
// gateway status.
//
// When every attempt fails the last error is wrapped in ErrRetriesExhausted so
// the caller can distinguish a sustained failure from a one-off.
func (c *sentryClient) issueRefWithErrorRetry(
	ctx context.Context,
	eventID string,
) (IssueRef, error) {
	var remaining = sentryRetryBudget
	var lastErr error

	for attempt := 1; attempt <= sentryMaxAttempts; attempt++ {
		ref, hint, err := c.issueRefOnce(ctx, eventID)
		if err == nil {
			return ref, nil
		}
		// A permanent outcome (404, 500, unparseable body) ends the loop on
		// the spot — retrying cannot change it.
		if hint == nil {
			return IssueRef{}, err
		}
		lastErr = err

		if attempt == sentryMaxAttempts {
			break
		}
		wait := backoffFor(attempt, hint.retryAfter)
		if wait > remaining {
			// Honouring this would blow the budget; stop rather than hold the
			// record. Reported as exhausted because that is what happened.
			glog.V(2).Infof(
				"sentry retry budget exhausted eventID=%s attempt=%d wait=%v remaining=%v",
				eventID, attempt, wait, remaining,
			)
			break
		}
		if waitErr := c.waiter.Wait(ctx, libtime.Duration(wait)); waitErr != nil {
			// Context cancelled mid-backoff — the consume loop is shutting
			// down, so surface that rather than the Sentry error.
			return IssueRef{}, errors.Wrap(ctx, waitErr, "wait before sentry retry")
		}
		remaining -= wait
		glog.V(2).Infof(
			"sentry lookup retry eventID=%s attempt=%d waited=%v",
			eventID, attempt, wait,
		)
	}

	return IssueRef{}, errors.Wrapf(
		ctx, ErrRetriesExhausted, "sentry lookup failed after retries: %v", lastErr,
	)
}

// backoffFor returns the wait before the next attempt: Sentry's own
// Retry-After when it supplied one, otherwise exponential backoff from
// sentryBaseBackoff (100ms, 200ms, …). The server's hint wins because it knows
// its own window.
func backoffFor(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return sentryBaseBackoff << (attempt - 1)
}

// retryHint marks an attempt as retryable and carries Sentry's Retry-After
// when the response supplied one (zero means "no hint, use backoff").
type retryHint struct {
	retryAfter time.Duration
}

// issueRefOnce performs exactly one lookup. A non-nil retryHint means the
// failure is transient and worth another attempt; a nil hint with a non-nil
// error means the outcome is final.
func (c *sentryClient) issueRefOnce(
	ctx context.Context,
	eventID string,
) (IssueRef, *retryHint, error) {
	path := c.eventIDsPath(eventID)
	start := time.Now()
	var status int
	var err error
	// Exactly one audit line per outbound call, whatever the outcome. The
	// token and the Authorization header are never logged.
	defer func() { logSentryRequest(path, status, start, err) }()

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if reqErr != nil {
		err = errors.Wrap(ctx, reqErr, "create sentry eventids request")
		return IssueRef{}, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, doErr := c.client.Do(req)
	if doErr != nil {
		err = errors.Wrap(ctx, doErr, "call sentry eventids")
		// Transport failure: connection refused, reset, timeout. Retryable —
		// unless the context itself is done, in which case retrying is futile.
		if ctx.Err() != nil {
			return IssueRef{}, nil, err
		}
		return IssueRef{}, &retryHint{}, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			glog.Warningf("close sentry response body failed err=%v", cerr)
		}
	}()
	status = resp.StatusCode

	if status == http.StatusNotFound {
		err = ErrEventNotFound
		return IssueRef{}, nil, err
	}
	if status != http.StatusOK {
		err = errors.Errorf(ctx, "unexpected sentry status %d", status)
		if isRetryableStatus(status) {
			return IssueRef{}, &retryHint{
				retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}, err
		}
		return IssueRef{}, nil, err
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		err = errors.Wrap(ctx, readErr, "read sentry response body")
		return IssueRef{}, nil, err
	}
	var parsed eventIDResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		err = errors.Wrap(ctx, jsonErr, "parse sentry response body")
		return IssueRef{}, nil, err
	}
	if parsed.Event.GroupID == "" {
		err = errors.Errorf(ctx, "sentry response carries no event.groupID")
		return IssueRef{}, nil, err
	}

	return IssueRef{
		NumericID: parsed.Event.GroupID,
		URL:       c.baseURL + "/issues/" + neturl.PathEscape(parsed.Event.GroupID) + "/",
	}, nil, nil
}

// isRetryableStatus reports whether an HTTP status is worth another attempt.
//
// 429 is the observed failure (burst rate limiting). 502/503/504 are genuine
// upstream/gateway trouble. A bare 500 is deliberately absent: per the
// Error Classification Patterns decision table some APIs return 500 for a
// missing resource, so retrying it burns quota on a permanent miss.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// parseRetryAfter reads the Retry-After header in both documented forms:
// delay-seconds ("2") and HTTP-date ("Wed, 21 Oct 2026 07:28:00 GMT").
// Returns 0 when absent, unparseable, or already in the past — the caller
// falls back to exponential backoff.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

// eventIDsPath builds the documented eventids lookup path for one event id.
// Both segments are escaped: the org is operator-supplied and the event id
// comes off the topic, i.e. from an untrusted sender.
func (c *sentryClient) eventIDsPath(eventID string) string {
	return "/api/0/organizations/" + neturl.PathEscape(c.org) +
		"/eventids/" + neturl.PathEscape(eventID) + "/"
}

// logSentryRequest emits the single audit line per outbound call: method, path,
// status and elapsed time. Only a genuine failure — transport error, unexpected
// status, unparseable body — logs at Warning; the 404 that means "Sentry never
// stored this event" is an expected outcome, not an incident.
func logSentryRequest(path string, status int, start time.Time, err error) {
	elapsed := time.Since(start).Milliseconds()
	if err != nil && !errors.Is(err, ErrEventNotFound) {
		glog.Warningf("http GET %s status=%d elapsed_ms=%d err=%v", path, status, elapsed, err)
		return
	}
	glog.V(2).Infof("http GET %s status=%d elapsed_ms=%d", path, status, elapsed)
}
