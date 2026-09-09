// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkg provides the core domain types and logic for the
// sentry-alert-consumer service:
//
//   - Record — the tee's {body, project, received_at, outcome} topic message
//   - Envelope — the raw Sentry envelope identity (event_id + event payload)
//   - Issue — the envelope-derived grouping identity (issue key, title, date)
//   - Consumer — the Kafka consume loop: record → issue → CreateTaskCommand
//   - TaskPublisher — sends the CreateTaskCommand for the sentry-issue-analyzer agent
//   - Metrics — per-record counters plus the last-event silence signal
//
// Logging is github.com/golang/glog (not log/slog) by deliberate choice: it
// matches every sibling watcher repo, and the maintainer Helm chart passes the
// glog `-v=<level>` verbosity flag to every watcher container, which only
// exists because glog registers it. Retaining glog keeps the family consistent
// and the chart's `-v` arg functional.
//
// See the task "Analyse Sentry Alerts Directly From the Kafka Topic" §
// Consumer Contract for the envelope-derived grouping contract.
package pkg

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate
