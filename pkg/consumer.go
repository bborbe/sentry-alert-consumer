// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
	"github.com/golang/glog"
)

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
	cfg       TaskConfig

	mu       sync.Mutex
	consumed map[int32]int64 // highest consumed offset per partition
}

// NewConsumer returns a Consumer publishing through publisher and counting
// through metrics.
func NewConsumer(publisher TaskPublisher, metrics Metrics, cfg TaskConfig) *Consumer {
	return &Consumer{
		metrics:   metrics,
		publisher: publisher,
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

	issue, err := BuildIssue(ctx, env, record)
	if err != nil {
		c.metrics.IncMessages("build_error")
		glog.Errorf("build issue failed project=%s err=%v", record.Project, err)
		return nil
	}

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
