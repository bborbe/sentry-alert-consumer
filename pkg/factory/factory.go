// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the sentry-alert-consumer
// binary.
package factory

import (
	task "github.com/bborbe/agent/command/task"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"

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

// CreateConsumer wires the publisher + metrics + task config into a ready
// Consumer. Pure composition — no I/O.
func CreateConsumer(
	sender task.CreateCommandSender,
	metrics pkg.Metrics,
	cfg pkg.TaskConfig,
) *pkg.Consumer {
	publisher := pkg.NewTaskPublisher(sender, metrics)
	return pkg.NewConsumer(publisher, metrics, cfg)
}
