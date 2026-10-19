// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/IBM/sarama"
	task "github.com/bborbe/agent/command/task"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/mocks"
	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("Consumer", func() {
	ctx := context.Background()
	cfg := pkg.TaskConfig{
		Stage:    "dev",
		Assignee: "sentry-analyzer-agent",
		Status:   "in_progress",
		Phase:    "planning",
	}

	msg := func(value string) *sarama.ConsumerMessage {
		return &sarama.ConsumerMessage{Value: []byte(value)}
	}

	// noEnrichment is the enrichment-off client: every lookup reports the event
	// as unknown to Sentry. The derived-key baseline these specs assert.
	noEnrichment := func() *mocks.SentryClient {
		fake := &mocks.SentryClient{}
		fake.IssueRefReturns(pkg.IssueRef{}, pkg.ErrEventNotFound)
		return fake
	}

	// wallClock is the injected clock for specs that assert something other than
	// the logged offset: the consumer must read a clock, and passing the real one
	// keeps those specs exercising the production wiring.
	wallClock := libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
		return libtime.DateTime(time.Now())
	})

	// forwardedRecord is a forwarded event whose envelope carries nothing but
	// an event_id, so the derived key is the per-event "event-<id>" fallback.
	forwardedRecord := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`

	ginkgo.It("publishes one command per processed record", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return true
		}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		err := c.ConsumeMessage(ctx, msg(forwardedRecord))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
		_, cmd := publisher.PublishCreateArgsForCall(0)
		gomega.Expect(cmd.Title).To(gomega.ContainSubstring("Analyze Sentry issue"))
		// enrich_miss first (noEnrichment reports the event unknown to Sentry),
		// then the publish decision.
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("enrich_miss"))
		gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("processed"))
		gomega.Expect(metrics.IncEventsArgsForCall(0)).To(gomega.Equal("forwarded"))
		gomega.Expect(metrics.SetLastEventCallCount()).To(gomega.Equal(1))
	})

	ginkgo.It("publishes nothing for a rejected record", func() {
		// The regression this guards: a rejected event was never stored by Sentry,
		// so it has no issue identity and deriveIssueKey falls back to the
		// per-event "event-<EventID>" key, which nothing can dedup — one analyzer
		// task per rejected event, measured 2026-10-18 at ~73 of 74 records.
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		sentry := noEnrichment()
		c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

		record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"rejected"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(0))
		// The outcome stays observable even though nothing is published.
		gomega.Expect(metrics.IncEventsArgsForCall(0)).To(gomega.Equal("rejected"))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("skipped_non_forwarded"))
		gomega.Expect(metrics.SetLastEventCallCount()).To(gomega.Equal(1))
	})

	ginkgo.It("publishes nothing for an upstream_error record", func() {
		// Same class as rejected: Sentry never stored the event, so there is no
		// issue to analyse and no stable key to dedup on.
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"upstream_error"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(metrics.IncEventsArgsForCall(0)).To(gomega.Equal("upstream_error"))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("skipped_non_forwarded"))
	})

	ginkgo.It("skips the sentry-heartbeat liveness event without a task", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		sentry := noEnrichment()
		c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

		record := `{"body":"{\"event_id\":\"hb1\",\"sent_at\":\"2026-10-10T16:00:00Z\"}\n{\"type\":\"event\",\"length\":10}\n{\"event_id\":\"hb1\",\"level\":\"error\",\"logger\":\"sentry-heartbeat\",\"message\":\"sentry-heartbeat\",\"tags\":{\"source\":\"sentry-heartbeat\"}}\n","project":"4505397462827008","received_at":"2026-10-10T16:00:04Z","outcome":"forwarded"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(0))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("skipped_heartbeat"))
	})

	ginkgo.It("counts and skips a record that fails to parse", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		err := c.ConsumeMessage(ctx, msg(`not-json`))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("parse_error"))
	})

	ginkgo.It("counts an unparseable event payload while still analysing the record", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateReturns(true)
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		// The record and the envelope's "event" item header both parse; only the
		// payload line does not. Before this counter the record fell silently to
		// the per-event identity with nothing observable — the shape that hid the
		// bare-array exception (v0.1.6) and bare-string message (PR #11) defects.
		record := `{"body":"{\"event_id\":\"abc123\"}\n{\"type\":\"event\"}\nnot-json","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		// Counted, not fatal: the task is still published on its derived key.
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
		_, cmd := publisher.PublishCreateArgsForCall(0)
		gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("payload_parse_error"))
		// enrich_miss (noEnrichment reports the event unknown) sits between the
		// parse counter and the publish decision.
		gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("enrich_miss"))
		gomega.Expect(metrics.IncMessagesArgsForCall(2)).To(gomega.Equal("processed"))
	})

	ginkgo.It("does not count a well-formed payload as a parse failure", func() {
		// The negative control: without it, an implementation that increments on
		// every message would satisfy the spec above just as well.
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateReturns(true)
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		record := `{"body":"{\"event_id\":\"abc123\"}\n{\"type\":\"event\"}\n{\"event_id\":\"abc123\",\"level\":\"error\",\"message\":\"boom\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		gomega.Expect(metrics.IncMessagesCallCount()).To(gomega.Equal(2))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("enrich_miss"))
		gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("processed"))
	})

	ginkgo.It("does not fail the loop when publishing fails", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return false
		}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment(), wallClock)

		err := c.ConsumeMessage(ctx, msg(forwardedRecord))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		// This consumer runs the no-enrichment client, so the miss is counted
		// first; the publish failure this spec is about follows it.
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("enrich_miss"))
		gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("publish_error"))
	})

	ginkgo.Describe("enrichment", func() {
		ginkgo.It("emits the numeric sentry issue id for a forwarded event", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{
				NumericID: "42",
				URL:       "https://bborbe.sentry.io/issues/42/",
			}, nil)
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
			_, eventID, _ := sentry.IssueRefArgsForCall(0)
			gomega.Expect(eventID).To(gomega.Equal("abc123"))

			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("42"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).
				To(gomega.Equal("https://bborbe.sentry.io/issues/42/"))
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("processed"))
		})

		ginkgo.It("re-keys the published command on the resolved issue id (SC1)", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{
				NumericID: "42",
				URL:       "https://bborbe.sentry.io/issues/42/",
			}, nil)
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)

			// The discriminator. `short_id` is NOT asserted here: it already
			// carries the numeric id on the unfixed build (taskbuilder reads
			// NumericID directly), so it cannot tell the fix from no fix. Both
			// fields below derive from Issue.Key, which is "event-abc123" before
			// the re-key and "42" after it.
			gomega.Expect(string(cmd.TaskIdentifier)).To(gomega.Equal(
				pkg.DeriveTaskIDKafka("42", "2026-09-09").String(),
			))
			gomega.Expect(cmd.Title).To(gomega.Equal("Analyze Sentry issue 42 - 2026-09-09"))
		})

		ginkgo.It("keeps the derived key when sentry does not know the event", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := noEnrichment()
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).To(gomega.Equal(""))
			// The miss is counted before the record is published: it is the
			// outcome this path exists to eliminate, so it is observable rather
			// than a V(2) line only. The derived key is still correct — a miss
			// is control flow, not a failure.
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("enrich_miss"))
			gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("processed"))
		})

		ginkgo.It("publishes the derived key when enrichment fails", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{}, errors.New(ctx, "connection refused"))
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).To(gomega.Equal(""))
			gomega.Expect(metrics.IncMessagesCallCount()).To(gomega.Equal(2))
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("enrich_error"))
			gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("processed"))
		})

		ginkgo.It("counts an exhausted retry apart from a one-off failure", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{}, errors.Wrapf(
				ctx, pkg.ErrRetriesExhausted, "sentry lookup failed after retries",
			))
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry, wallClock)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			// Still best-effort: the task is published with its derived key.
			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
			// …but the sustained failure lands on its own label value.
			gomega.Expect(metrics.IncMessagesCallCount()).To(gomega.Equal(2))
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).
				To(gomega.Equal("enrich_retry_exhausted"))
			gomega.Expect(metrics.IncMessagesArgsForCall(1)).To(gomega.Equal("processed"))
		})
	})
})

// capturedStderr runs fn with glog redirected into a pipe and returns what was
// written. Both flags are set because the enriched line is a V(2) line: without
// -v=2 it is never emitted, and a capture that reads nothing would make an
// absence assertion pass while proving nothing.
func capturedStderr(fn func()) string {
	_ = flag.Set("logtostderr", "true")
	_ = flag.Set("v", "2")

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	fn()
	glog.Flush()
	os.Stderr = oldStderr
	_ = w.Close()
	out, _ := io.ReadAll(r)
	_ = r.Close()
	return string(out)
}

// eventAgeFrom pulls the eventAge value off the enriched line. The line exists
// for a machine probe, so it is grepped rather than read by adjacency.
func eventAgeFrom(captured string) string {
	for _, line := range strings.Split(captured, "\n") {
		if !strings.Contains(line, "enriched ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "eventAge=") {
				return strings.TrimPrefix(field, "eventAge=")
			}
		}
	}
	return ""
}

var _ = ginkgo.Describe("enriched eventAge", func() {
	ctx := context.Background()
	cfg := pkg.TaskConfig{Stage: "dev"}

	// The 404 schedule lives inside the real SentryClient (pkg/sentryclient.go),
	// NOT in the consumer: enrichIssue calls IssueRef exactly once and the client
	// runs the 404,404,200 sequence internally. So the mock stands in for the
	// whole schedule — it advances the injected clock by the waits it would have
	// spent, then succeeds. That advance is the point: the clock the consumer
	// reads *before* the call is untouched by it, while a clock read *after* the
	// call has absorbed all six seconds. A mock that returned immediately would
	// leave both readings identical and the spec would prove nothing.
	const scheduleWaits = 6 * time.Second
	const firstAttemptOffset = 2 * time.Second

	eventTime := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)

	record := func() string {
		return fmt.Sprintf(
			`{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":%q,"outcome":"forwarded"}`,
			eventTime.Format(time.RFC3339),
		)
	}

	msg := func(value string) *sarama.ConsumerMessage {
		return &sarama.ConsumerMessage{Value: []byte(value)}
	}

	// scheduleSentry advances clock past the schedule's waits, then succeeds —
	// the 404,404,200 sequence as the consumer sees it (one call, some elapsed
	// time). Call count is asserted as 1 precisely because the retries are the
	// client's business, not the consumer's.
	scheduleSentry := func(clock *time.Time) *mocks.SentryClient {
		sentry := &mocks.SentryClient{}
		sentry.IssueRefStub = func(_ context.Context, _ string, _ time.Time) (pkg.IssueRef, error) {
			*clock = clock.Add(scheduleWaits)
			return pkg.IssueRef{NumericID: "42", URL: "https://bborbe.sentry.io/issues/42/"}, nil
		}
		return sentry
	}

	ginkgo.It("reports the first-attempt offset, not the completion offset (SC1)", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateReturns(true)
		clock := eventTime.Add(firstAttemptOffset)
		sentry := scheduleSentry(&clock)
		getter := libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
			return libtime.DateTime(clock)
		})
		c := pkg.NewConsumer(publisher, metrics, cfg, sentry, getter)

		captured := capturedStderr(func() {
			gomega.Expect(c.ConsumeMessage(ctx, msg(record()))).
				To(gomega.Not(gomega.HaveOccurred()))
		})

		gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
		// The capture is live, not silently empty.
		gomega.Expect(captured).To(gomega.ContainSubstring("enriched "))
		// The offset at the call, with the six seconds of schedule waits excluded.
		gomega.Expect(eventAgeFrom(captured)).To(gomega.Equal("2s"))
		// The completion offset would have absorbed the waits — this is the
		// reading the defect produced, so it must not appear.
		gomega.Expect(captured).To(gomega.Not(gomega.ContainSubstring("eventAge=8s")))
	})

	ginkgo.It("still reports the true age of a replayed event (SC2)", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateReturns(true)
		// An event older than 30 s skips the 404 schedule, so the offset at the
		// call and after it coincide: this case guards the replay-burst behaviour
		// rather than the repair, and is labelled as such in the task.
		clock := eventTime.Add(60 * time.Second)
		sentry := &mocks.SentryClient{}
		sentry.IssueRefReturns(pkg.IssueRef{
			NumericID: "42",
			URL:       "https://bborbe.sentry.io/issues/42/",
		}, nil)
		getter := libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
			return libtime.DateTime(clock)
		})
		c := pkg.NewConsumer(publisher, metrics, cfg, sentry, getter)

		captured := capturedStderr(func() {
			gomega.Expect(c.ConsumeMessage(ctx, msg(record()))).
				To(gomega.Not(gomega.HaveOccurred()))
		})

		gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
		gomega.Expect(captured).To(gomega.ContainSubstring("enriched "))
		gomega.Expect(eventAgeFrom(captured)).To(gomega.Equal("1m0s"))
	})
})
