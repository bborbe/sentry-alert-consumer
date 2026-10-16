// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the sentry-alert-consumer
// binary.
package factory

import (
	"context"
	"strings"
	"time"

	task "github.com/bborbe/agent/command/task"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

// CreateKafkaSender constructs a typed create-task command sender backed by a
// Kafka sync producer. defaultVault is the Obsidian vault slug substituted
// into each command's TargetVault when unset (empty = controller default).
func CreateKafkaSender(
	syncProducer libkafka.SyncProducer,
	topicPrefix base.TopicPrefix,
	defaultVault string,
) task.CreateCommandSender {
	sender := cdb.NewCommandObjectSender(syncProducer, topicPrefix, log.DefaultSamplerFactory)
	return task.NewCreateCommandSender(sender, defaultVault)
}

// CreateConsumer wires the publisher + metrics + task config + enrichment
// client + clock into a ready Consumer. Pure composition — no I/O, and the
// clock arrives as a parameter rather than being created here so the caller
// owns which clock the consumer reads.
func CreateConsumer(
	sender task.CreateCommandSender,
	metrics pkg.Metrics,
	cfg pkg.TaskConfig,
	sentry pkg.SentryClient,
	currentDateTimeGetter libtime.CurrentDateTimeGetter,
) *pkg.Consumer {
	publisher := pkg.NewTaskPublisher(sender, metrics)
	return pkg.NewConsumer(publisher, metrics, cfg, sentry, currentDateTimeGetter)
}

// CreateSentryClient returns the event→issue enrichment client. An empty token
// disables enrichment — the binary still runs, every lookup reports the event
// as unknown, and tasks keep their envelope-derived key — and the disabled
// state is logged once at startup.
func CreateSentryClient(url, org, token string) pkg.SentryClient {
	if strings.TrimSpace(token) == "" {
		glog.Warningf(
			"SENTRY_API_TOKEN empty — event→issue enrichment disabled, tasks keep the derived key",
		)
		return disabledSentryClient{}
	}
	return pkg.NewSentryClient(url, org, token)
}

// disabledSentryClient is the enrichment-off SentryClient: it resolves nothing
// and reports every event as unknown, so the consumer behaves exactly as it did
// before enrichment existed.
type disabledSentryClient struct{}

// IssueRef reports every event as unknown to Sentry.
func (disabledSentryClient) IssueRef(
	_ context.Context,
	_ string,
	_ time.Time,
) (pkg.IssueRef, error) {
	return pkg.IssueRef{}, pkg.ErrEventNotFound
}
