// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/bborbe/errors"
	"github.com/golang/glog"
)

// outcomeForwarded is the tee outcome that means Sentry accepted the event —
// the only outcome worth an eventids lookup.
const outcomeForwarded = "forwarded"

// heartbeatLogger is the logger the sentry-heartbeat emitter (sentry-watcher
// pkg.HeartbeatTag) stamps on its synthetic liveness event. The heartbeat
// proves ingestion is alive; it is not a bug, so it never becomes a task.
const heartbeatLogger = "sentry-heartbeat"

// Consumer processes messages from the {branch}-raw-sentry-alert-input topic:
// parse the tee record, derive the envelope-based issue identity, and publish
// one CreateTaskCommand per (issue-key, date).
//
// The handler is deliberately never fatal: a record that fails to parse still
// yields a task (fallback identities exist at every layer) and the consume
// loop must not die on a single bad message — controller dedup makes re-emits
// no-ops, so replaying a bad batch later is safe.
type Consumer struct {
	metrics   Metrics
	publisher TaskPublisher
	sentry    SentryClient
	cfg       TaskConfig

	mu       sync.Mutex
	consumed map[int32]int64 // highest consumed offset per partition
}

// NewConsumer returns a Consumer publishing through publisher, counting through
// metrics and enriching forwarded events through sentry.
func NewConsumer(
	publisher TaskPublisher,
	metrics Metrics,
	cfg TaskConfig,
	sentry SentryClient,
) *Consumer {
	return &Consumer{
		metrics:   metrics,
		publisher: publisher,
		sentry:    sentry,
		cfg:       cfg,
		consumed:  make(map[int32]int64),
	}
}

// ConsumedOffsets returns a copy of the highest consumed offset per partition
// — the input for the lag tracker (highwater minus consumed).
func (c *Consumer) ConsumedOffsets() map[int32]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[int32]int64, len(c.consumed))
	for partition, offset := range c.consumed {
		out[partition] = offset
	}
	return out
}

// ConsumeMessage implements libkafka.MessageHandler (one call per topic
// message). It never returns a non-nil error to the consume loop: a per-record
// failure is counted and skipped, keeping the stream alive.
func (c *Consumer) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	c.mu.Lock()
	if offset := c.consumed[msg.Partition]; msg.Offset > offset {
		c.consumed[msg.Partition] = msg.Offset
	}
	c.mu.Unlock()

	record, err := ParseRecord(ctx, msg.Value)
	if err != nil {
		c.metrics.IncMessages("parse_error")
		glog.Errorf("parse record failed err=%v", err)
		return nil
	}
	c.metrics.IncEvents(record.Outcome)
	c.metrics.SetLastEvent(record.ReceivedAt)

	env, err := ParseEnvelope(ctx, record.Body)
	if err != nil {
		c.metrics.IncMessages("envelope_error")
		glog.Errorf("parse envelope failed project=%s err=%v", record.Project, err)
		// Fall through with the empty envelope: BuildIssue falls back to a
		// body-hash identity, so the record is still analysed, never dropped.
		env = Envelope{EventID: ""}
	}
	if env.PayloadParseFailed {
		// The "event" item was present but its payload did not unmarshal. The
		// record still becomes a task on a fallback identity, so without this
		// counter the failure is invisible — which is how two SDK shapes ran
		// undetected for weeks (bare-array exception, v0.1.6; bare-string
		// message, PR #11). Counted, not fatal: the consume loop must not die
		// on a single bad payload.
		c.metrics.IncMessages("payload_parse_error")
		glog.Warningf(
			"parse event payload failed eventID=%s project=%s",
			env.EventID,
			record.Project,
		)
	}

	if env.Event != nil && env.Event.Logger == heartbeatLogger {
		c.metrics.IncMessages("skipped_heartbeat")
		glog.V(2).Infof("skip heartbeat eventID=%s project=%s", env.EventID, record.Project)
		return nil
	}

	issue, err := BuildIssue(ctx, env, record)
	if err != nil {
		c.metrics.IncMessages("build_error")
		glog.Errorf("build issue failed project=%s err=%v", record.Project, err)
		return nil
	}

	c.enrichIssue(ctx, &issue)

	cmd := BuildCreateCommand(issue, c.cfg)
	if !c.publisher.PublishCreate(ctx, cmd) {
		c.metrics.IncMessages("publish_error")
		return nil
	}
	c.metrics.IncMessages("processed")
	glog.V(2).Infof(
		"processed record issue=%s project=%s outcome=%s date=%s",
		issue.Key, issue.Project, issue.Outcome, issue.Date,
	)
	return nil
}

// enrichIssue resolves the numeric Sentry issue id for one derived issue and
// writes it back. Only forwarded events are looked up: a rejected (or
// upstream_error) event was never stored by Sentry, so its lookup is a
// guaranteed 404 that burns API quota.
//
// Enrichment is best-effort by contract — a record is never dropped because
// enrichment failed. A miss keeps the derived key (the forwarded-but-dropped-by-
// quota case this enrichment exists for); any other failure is counted and
// logged and the task is still published with the derived key.
func (c *Consumer) enrichIssue(ctx context.Context, issue *Issue) {
	if issue.Outcome != outcomeForwarded || issue.EventID == "" {
		return
	}
	// The event's own age gates the client's dedicated 404 schedule: a 404 on
	// an event replayed from the topic long after Sentry indexed it is a real
	// miss, not an indexing race, and must not hold the partition for 10 s.
	// An unparseable timestamp resolves to the zero time, which the client
	// reads as "old" — the conservative direction, since the alternative is
	// spending the schedule's wait on an age we cannot establish.
	eventTime, parseErr := time.Parse(time.RFC3339, issue.EventTimestamp)
	if parseErr != nil {
		glog.Warningf(
			"enrichment event timestamp unparseable eventID=%s timestamp=%q err=%v",
			issue.EventID,
			issue.EventTimestamp,
			parseErr,
		)
	}
	ref, err := c.sentry.IssueRef(ctx, issue.EventID, eventTime)
	if err != nil {
		if errors.Is(err, ErrEventNotFound) {
			// A miss stays control flow — the derived key is still correct —
			// but it is the outcome this whole path exists to eliminate, so it
			// is counted rather than logged only. Without the counter the
			// dashboard reads healthy while every lookup misses.
			c.metrics.IncMessages("enrich_miss")
			glog.V(2).Infof(
				"enrichment miss eventID=%s issue=%s — keeping derived key",
				issue.EventID,
				issue.Key,
			)
			return
		}
		// Split the outcome: a sustained failure (the client retried and gave
		// up) means Sentry is rate-limiting or down and is worth alerting on;
		// a single non-retryable failure is noise by comparison.
		result := "enrich_error"
		if errors.Is(err, ErrRetriesExhausted) {
			result = "enrich_retry_exhausted"
		}
		c.metrics.IncMessages(result)
		glog.Warningf(
			"enrichment failed eventID=%s issue=%s result=%s err=%v",
			issue.EventID,
			issue.Key,
			result,
			err,
		)
		return
	}
	issue.NumericID = ref.NumericID
	issue.IssueURL = ref.URL
	// eventID and eventAge exist for the live probe, not for humans reading
	// one line: the retry line carries the eventID, so the two correlate by
	// key rather than by log adjacency, and eventAge is the *lookup* offset —
	// measured from the event's own creation, before the retry waits — rather
	// than the completion offset, which would fold the +1/+3/+6/+10 s schedule
	// into the number and make a correct fix look slow.
	glog.V(2).Infof(
		"enriched issue=%s eventID=%s numericID=%s eventAge=%s",
		issue.Key,
		issue.EventID,
		ref.NumericID,
		time.Since(eventTime).Round(time.Millisecond),
	)
}
