// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"

	"github.com/IBM/sarama"
	task "github.com/bborbe/agent/command/task"
	"github.com/bborbe/errors"
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

	// forwardedRecord is a forwarded event whose envelope carries nothing but
	// an event_id, so the derived key is the per-event "event-<id>" fallback.
	forwardedRecord := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`

	ginkgo.It("publishes one command per processed record", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return true
		}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment())

		record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"rejected"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
		_, cmd := publisher.PublishCreateArgsForCall(0)
		gomega.Expect(cmd.Title).To(gomega.ContainSubstring("Analyze Sentry issue"))
		gomega.Expect(metrics.IncMessagesCallCount()).To(gomega.Equal(1))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("processed"))
		gomega.Expect(metrics.IncEventsArgsForCall(0)).To(gomega.Equal("rejected"))
		gomega.Expect(metrics.SetLastEventCallCount()).To(gomega.Equal(1))
	})

	ginkgo.It("counts and skips a record that fails to parse", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment())

		err := c.ConsumeMessage(ctx, msg(`not-json`))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("parse_error"))
	})

	ginkgo.It("analyses a rejected alert even when the envelope payload is missing", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment())

		record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"rejected"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
		gomega.Expect(metrics.IncEventsArgsForCall(0)).To(gomega.Equal("rejected"))
	})

	ginkgo.It("does not fail the loop when publishing fails", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return false
		}
		c := pkg.NewConsumer(publisher, metrics, cfg, noEnrichment())

		err := c.ConsumeMessage(ctx, msg(forwardedRecord))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("publish_error"))
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
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
			_, eventID := sentry.IssueRefArgsForCall(0)
			gomega.Expect(eventID).To(gomega.Equal("abc123"))

			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("42"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).
				To(gomega.Equal("https://bborbe.sentry.io/issues/42/"))
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("processed"))
		})

		ginkgo.It("keeps the derived key when sentry does not know the event", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := noEnrichment()
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry)

			err := c.ConsumeMessage(ctx, msg(forwardedRecord))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(1))
			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).To(gomega.Equal(""))
			gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("processed"))
		})

		ginkgo.It("never calls sentry for a rejected event", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{
				NumericID: "42",
				URL:       "https://bborbe.sentry.io/issues/42/",
			}, nil)
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry)

			record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"rejected"}`
			err := c.ConsumeMessage(ctx, msg(record))
			gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))

			gomega.Expect(sentry.IssueRefCallCount()).To(gomega.Equal(0))
			gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(1))
			_, cmd := publisher.PublishCreateArgsForCall(0)
			gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("event-abc123"))
			gomega.Expect(cmd.Frontmatter["issue_url"]).To(gomega.Equal(""))
		})

		ginkgo.It("publishes the derived key when enrichment fails", func() {
			metrics := &mocks.Metrics{}
			publisher := &mocks.TaskPublisher{}
			publisher.PublishCreateReturns(true)
			sentry := &mocks.SentryClient{}
			sentry.IssueRefReturns(pkg.IssueRef{}, errors.New(ctx, "connection refused"))
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry)

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
			c := pkg.NewConsumer(publisher, metrics, cfg, sentry)

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
