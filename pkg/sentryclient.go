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
	"strings"
	"time"

	"github.com/bborbe/errors"
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

// SentryClient resolves the Sentry-side identity of one event id. The consumer
// enriches only forwarded events: a rejected event was never forwarded to
// Sentry, so its lookup is a guaranteed 404 that burns API quota on a
// quota-limited org where rejected is the majority of traffic.
type SentryClient interface {
	// IssueRef resolves eventID to its Sentry issue. Returns ErrEventNotFound
	// when Sentry holds no record of the event.
	IssueRef(ctx context.Context, eventID string) (IssueRef, error)
}

// sentryClientTimeout bounds one lookup so a slow Sentry cannot stall the
// Kafka consume loop indefinitely.
const sentryClientTimeout = 10 * time.Second

// NewSentryClient returns the HTTP SentryClient for the documented eventids
// endpoint ({url}/api/0/organizations/{org}/eventids/{event_id}/, scope
// org:read). Constructor-injected: no package-level state, and the token lives
// only in memory.
func NewSentryClient(url, org, token string) SentryClient {
	return &sentryClient{
		baseURL: strings.TrimSuffix(url, "/"),
		org:     org,
		token:   token,
		client:  &http.Client{Timeout: sentryClientTimeout},
	}
}

type sentryClient struct {
	baseURL string
	org     string
	token   string
	client  *http.Client
}

// eventIDResponse is the documented 200 body of the eventids endpoint: the
// event's internal group (issue) id, with the project slug alongside it.
type eventIDResponse struct {
	Event struct {
		GroupID string `json:"groupID"`
	} `json:"event"`
	ProjectSlug string `json:"projectSlug"`
}

// IssueRef implements SentryClient against the eventids endpoint.
func (c *sentryClient) IssueRef(ctx context.Context, eventID string) (IssueRef, error) {
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
		return IssueRef{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, doErr := c.client.Do(req)
	if doErr != nil {
		err = errors.Wrap(ctx, doErr, "call sentry eventids")
		return IssueRef{}, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			glog.Warningf("close sentry response body failed err=%v", cerr)
		}
	}()
	status = resp.StatusCode

	if status == http.StatusNotFound {
		err = ErrEventNotFound
		return IssueRef{}, err
	}
	if status != http.StatusOK {
		err = errors.Errorf(ctx, "unexpected sentry status %d", status)
		return IssueRef{}, err
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		err = errors.Wrap(ctx, readErr, "read sentry response body")
		return IssueRef{}, err
	}
	var parsed eventIDResponse
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		err = errors.Wrap(ctx, jsonErr, "parse sentry response body")
		return IssueRef{}, err
	}
	if parsed.Event.GroupID == "" {
		err = errors.Errorf(ctx, "sentry response carries no event.groupID")
		return IssueRef{}, err
	}

	return IssueRef{
		NumericID: parsed.Event.GroupID,
		URL:       c.baseURL + "/issues/" + neturl.PathEscape(parsed.Event.GroupID) + "/",
	}, nil
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
