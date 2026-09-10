// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"time"

	"github.com/IBM/sarama"
	task "github.com/bborbe/agent/command/task"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/mocks"
	"github.com/bborbe/sentry-alert-consumer/pkg"
)

type fakeTicker struct{}

func (fakeTicker) Chan() <-chan time.Time { return nil }

func (fakeTicker) Stop() {}

type fakeLogger struct{}

func (fakeLogger) Errorf(_ string, _ ...any) {}

var _ = ginkgo.Describe("LagTracker", func() {
	ctx := context.Background()
	cfg := pkg.TaskConfig{Stage: "dev", Assignee: "sentry-analyzer-agent"}

	record := `{"body":"{\"event_id\":\"abc123\"}","project":"nuke-prod","received_at":"2026-09-09T20:10:00Z","outcome":"forwarded"}`

	consumedAt := func(partition int32, offset int64) *pkg.Consumer {
		metrics := &mocks.Metrics{}
		publisher := &mocks.TaskPublisher{}
		publisher.PublishCreateStub = func(_ context.Context, _ task.CreateCommand) bool {
			return true
		}
		c := pkg.NewConsumer(publisher, metrics, cfg)
		msg := &sarama.ConsumerMessage{Partition: partition, Offset: offset, Value: []byte(record)}
		gomega.Expect(c.ConsumeMessage(ctx, msg)).To(gomega.Succeed())
		return c
	}

	ginkgo.It("reports highwater minus consumed as the lag", func() {
		offsets := &mocks.OffsetGetter{}
		offsets.GetOffsetStub = func(_ string, partition int32, _ int64) (int64, error) {
			return 45, nil
		}
		metrics := &mocks.Metrics{}
		tracker := pkg.NewLagTracker(
			offsets,
			"develop-raw-sentry-alert-input",
			consumedAt(0, 41),
			metrics,
			fakeTicker{},
			fakeLogger{},
		)

		tracker.Update(ctx)

		gomega.Expect(metrics.SetKafkaLagCallCount()).To(gomega.Equal(1))
		partition, lag := metrics.SetKafkaLagArgsForCall(0)
		gomega.Expect(partition).To(gomega.Equal(int32(0)))
		gomega.Expect(lag).To(gomega.Equal(float64(3))) // 45 - (41+1)
	})

	ginkgo.It("clamps a caught-up consumer to zero lag", func() {
		offsets := &mocks.OffsetGetter{}
		offsets.GetOffsetStub = func(_ string, partition int32, _ int64) (int64, error) {
			return 41, nil
		}
		metrics := &mocks.Metrics{}
		tracker := pkg.NewLagTracker(
			offsets,
			"develop-raw-sentry-alert-input",
			consumedAt(0, 41),
			metrics,
			fakeTicker{},
			fakeLogger{},
		)

		tracker.Update(ctx)

		_, lag := metrics.SetKafkaLagArgsForCall(0)
		gomega.Expect(lag).To(gomega.Equal(float64(0)))
	})

	ginkgo.It("skips a partition whose highwater cannot be fetched", func() {
		offsets := &mocks.OffsetGetter{}
		offsets.GetOffsetStub = func(_ string, partition int32, _ int64) (int64, error) {
			return 0, context.Canceled
		}
		metrics := &mocks.Metrics{}
		tracker := pkg.NewLagTracker(
			offsets,
			"develop-raw-sentry-alert-input",
			consumedAt(0, 41),
			metrics,
			fakeTicker{},
			fakeLogger{},
		)

		tracker.Update(ctx)

		gomega.Expect(metrics.SetKafkaLagCallCount()).To(gomega.Equal(0))
	})
})
