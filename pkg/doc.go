// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkg provides the core domain types and logic for the
// sentry-watcher service:
//
//   - SentryClient — queries the bborbe Sentry org for unresolved issues with recent activity
//   - Cursor — per-short-id last-emitted-date dedup state persisted to disk
//   - TaskPublisher — sends the CreateTaskCommand for the sentry-issue-analyzer agent
//   - Watcher — the Poll loop tying it all together
//
// Logging is github.com/golang/glog (not log/slog) by deliberate choice: this
// module was scaffolded from the go-version-watcher skeleton, which already
// used glog, and every sibling watcher repo (github-pr-watcher,
// github-build-watcher, github-release-watcher, go-version-watcher) uses glog —
// the maintainer Helm chart passes the glog `-v=<level>` verbosity flag to every
// watcher container, which only exists because glog registers it. The
// go-cli/slog-not-glog-in-new-projects rule exempts modules with prior glog
// usage; retaining glog keeps the family consistent and the chart's `-v` arg
// functional.
//
// See [[Watcher Writing Guide]] for the producer-side contract and [[Agent Task
// File Contract]] for the frontmatter/body shape this watcher emits.
package pkg

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate
