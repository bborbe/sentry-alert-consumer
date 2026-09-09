// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	agentlib "github.com/bborbe/agent"
	task "github.com/bborbe/agent/command/task"
)

// TaskConfig groups the per-task frontmatter envelope settings — identical
// shape to cmd/create-tasks' taskConfig (same defaults), so the emitted task
// contract is preserved byte-for-byte except for the identity source.
type TaskConfig struct {
	Stage    string
	Assignee string
	Status   string
	Phase    string
}

// BuildCreateCommand assembles the CreateTaskCommand for one derived Issue.
// The shape mirrors cmd/create-tasks' buildCreateCommand: same title pattern,
// same frontmatter keys, same task_type. The differences are deliberate and
// contract-documented:
//
//   - task_identifier derives from the Kafka issue key (UUID5 namespace
//     "kafka:" + issue-key + ":" + date) instead of the Sentry short-id, so a
//     derived task never collides with a collector-emitted task
//   - short_id carries the derived key until enrichment supplies the Sentry
//     short-id
//   - issue_url is empty (the envelope carries no permalink; enrichment owns
//     the mapping)
func BuildCreateCommand(issue Issue, cfg TaskConfig) task.CreateCommand {
	taskIDStr := DeriveTaskIDKafka(issue.Key, issue.Date).String()
	title := "Analyze Sentry issue " + issue.Key + " - " + issue.Date
	body := "# Analyze Sentry issue " + issue.Key + " - " + issue.Date + "\n\n" +
		"Kafka alert (received " + issue.ReceivedAt + ", outcome " + issue.Outcome +
		") in project " + issue.Project + ".\n" +
		"- Issue: " + issue.Key + "\n" +
		"- Project: " + issue.Project + "\n" +
		"- Title: " + issue.Title + "\n" +
		"- Event: " + issue.EventID
	return task.CreateCommand{
		TaskIdentifier: agentlib.TaskIdentifier(taskIDStr),
		Title:          title,
		Frontmatter: agentlib.TaskFrontmatter{
			"task_type":       "sentry-issue-analyzer",
			"assignee":        cfg.Assignee,
			"phase":           cfg.Phase,
			"status":          cfg.Status,
			"stage":           DeriveStage(issue.Project, cfg.Stage),
			"task_identifier": taskIDStr,
			"title":           title,
			"short_id":        issue.Key,
			"project":         issue.Project,
			"date":            issue.Date,
			"issue_url":       "",
		},
		Body: body,
	}
}
