// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"

	"github.com/IBM/sarama"
	task "github.com/bborbe/agent/command/task"
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

	ginkgo.It("publishes one command per processed record", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return true
		}
		c := pkg.NewConsumer(publisher, metrics, cfg)

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
		c := pkg.NewConsumer(publisher, metrics, cfg)

		err := c.ConsumeMessage(ctx, msg(`not-json`))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(publisher.PublishCreateCallCount()).To(gomega.Equal(0))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("parse_error"))
	})

	ginkgo.It("analyses a rejected alert even when the envelope payload is missing", func() {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		c := pkg.NewConsumer(publisher, metrics, cfg)

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
		c := pkg.NewConsumer(publisher, metrics, cfg)

		record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`
		err := c.ConsumeMessage(ctx, msg(record))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(metrics.IncMessagesArgsForCall(0)).To(gomega.Equal("publish_error"))
	})
})
