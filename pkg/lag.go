// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"time"

	"github.com/IBM/sarama"
)

//counterfeiter:generate -o ../mocks/offset-getter.go --fake-name OffsetGetter . OffsetGetter

// OffsetGetter retrieves broker-side offsets for a topic partition. It is a
// narrow slice of sarama.Client so the tracker is testable without a broker.
type OffsetGetter interface {
	GetOffset(topic string, partition int32, offset int64) (int64, error)
}

// Ticker fires on a fixed cadence. The concrete time.NewTicker-backed
// implementation lives at the call site so the tracker stays clock-free.
type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

// Logger receives the tracker's error output.
type Logger interface {
	Errorf(format string, args ...any)
}

// LagTracker reports the consumer's OWN Kafka lag per partition: broker
// highwater minus the highest consumed offset. It runs on a fixed cadence so
// the broken-consumer alert thresholds on this consumer's position — not the
// strimzi-topic-reader's per-read cache (which carries no consumer-group label
// and never reflects a dead sentry-alert-consumer).
type LagTracker struct {
	client   OffsetGetter
	topic    string
	consumer *Consumer
	metrics  Metrics
	ticker   Ticker
	logger   Logger
}

// NewLagTracker returns a LagTracker polling the broker highwater for every
// partition the consumer has consumed.
func NewLagTracker(
	client OffsetGetter,
	topic string,
	consumer *Consumer,
	metrics Metrics,
	ticker Ticker,
	logger Logger,
) *LagTracker {
	return &LagTracker{
		client:   client,
		topic:    topic,
		consumer: consumer,
		metrics:  metrics,
		ticker:   ticker,
		logger:   logger,
	}
}

// Run polls the lag until ctx is cancelled. The first pass runs immediately so
// the gauge is populated shortly after startup.
func (l *LagTracker) Run(ctx context.Context) error {
	defer l.ticker.Stop()
	l.Update(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-l.ticker.Chan():
			l.Update(ctx)
		}
	}
}

// Update computes lag for every consumed partition and records it as a gauge.
func (l *LagTracker) Update(ctx context.Context) {
	for partition, consumed := range l.consumer.ConsumedOffsets() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		highwater, err := l.client.GetOffset(l.topic, partition, sarama.OffsetNewest)
		if err != nil {
			l.logger.Errorf(
				"get highwater for topic %s partition %d failed err=%v",
				l.topic,
				partition,
				err,
			)
			continue
		}
		lag := highwater - (consumed + 1)
		if lag < 0 {
			lag = 0
		}
		l.metrics.SetKafkaLag(partition, float64(lag))
	}
}
