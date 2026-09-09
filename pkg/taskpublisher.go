// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"

	task "github.com/bborbe/agent/command/task"
	"github.com/golang/glog"
)

//counterfeiter:generate -o ../mocks/task_publisher.go --fake-name TaskPublisher . TaskPublisher

// CreateCommandSender mirrors task.CreateCommandSender (github.com/bborbe/agent)
// so its mock can be counterfeiter-generated in this module. taskPublisher uses
// the agent interface directly; this local alias exists only so the test can use
// a generated double instead of a hand-rolled fake (counterfeiter cannot target
// an interface in another module). The two are structurally identical.
type CreateCommandSender interface {
	SendCommand(ctx context.Context, cmd task.CreateCommand) error
}

//counterfeiter:generate -o ../mocks/task_create_command_sender.go --fake-name TaskCreateCommandSender . CreateCommandSender

// TaskPublisher sends a pre-built CreateTaskCommand via the supplied
// CreateCommandSender. Returns true on successful send, false on error.
type TaskPublisher interface {
	PublishCreate(ctx context.Context, cmd task.CreateCommand) bool
}

// NewTaskPublisher returns a TaskPublisher that wraps the given sender + metrics.
func NewTaskPublisher(sender task.CreateCommandSender, metrics Metrics) TaskPublisher {
	return &taskPublisher{sender: sender, metrics: metrics}
}

type taskPublisher struct {
	sender  task.CreateCommandSender
	metrics Metrics
}

func (p *taskPublisher) PublishCreate(ctx context.Context, cmd task.CreateCommand) bool {
	if err := p.sender.SendCommand(ctx, cmd); err != nil {
		glog.Errorf(
			"publish create-task failed taskID=%s title=%q err=%v",
			string(cmd.TaskIdentifier),
			cmd.Title,
			err,
		)
		p.metrics.IncPublished("error")
		return false
	}
	glog.V(2).Infof(
		"published CreateTaskCommand taskID=%s title=%q",
		string(cmd.TaskIdentifier),
		cmd.Title,
	)
	p.metrics.IncPublished("create")
	return true
}
