// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("BuildCreateCommand", func() {
	cfg := pkg.TaskConfig{
		Stage:    "dev",
		Assignee: "sentry-analyzer-agent",
		Status:   "in_progress",
		Phase:    "planning",
	}
	issue := pkg.Issue{
		Key:        "abc123def456",
		EventID:    "evt-1",
		Title:      "boom",
		Project:    "nuke-prod",
		Outcome:    "rejected",
		ReceivedAt: "2026-09-09T20:10:00Z",
		Date:       "2026-09-09",
	}

	ginkgo.It("builds the analyzer task contract", func() {
		cmd := pkg.BuildCreateCommand(issue, cfg)
		gomega.Expect(cmd.Title).To(gomega.Equal("Analyze Sentry issue abc123def456 - 2026-09-09"))
		gomega.Expect(cmd.Frontmatter["task_type"]).To(gomega.Equal("sentry-issue-analyzer"))
		gomega.Expect(cmd.Frontmatter["assignee"]).To(gomega.Equal("sentry-analyzer-agent"))
		gomega.Expect(cmd.Frontmatter["status"]).To(gomega.Equal("in_progress"))
		gomega.Expect(cmd.Frontmatter["phase"]).To(gomega.Equal("planning"))
		gomega.Expect(cmd.Frontmatter["short_id"]).To(gomega.Equal("abc123def456"))
		gomega.Expect(cmd.Frontmatter["project"]).To(gomega.Equal("nuke-prod"))
		gomega.Expect(cmd.Frontmatter["date"]).To(gomega.Equal("2026-09-09"))
		gomega.Expect(cmd.Frontmatter["stage"]).To(gomega.Equal("prod"))
		gomega.Expect(cmd.Frontmatter["issue_url"]).To(gomega.Equal(""))
		gomega.Expect(cmd.Body).To(gomega.ContainSubstring("outcome rejected"))
		gomega.Expect(cmd.Body).To(gomega.ContainSubstring("- Event: evt-1"))
	})

	ginkgo.It("carries the exception snapshot into the task body", func() {
		withTrace := issue
		inApp := true
		withTrace.ExceptionType = "ConnectionRefusedError"
		withTrace.ExceptionValue = "refused"
		withTrace.ExceptionModule = "mt5.connector.mt5linux"
		withTrace.Trace = []pkg.Frame{
			{Filename: "mt5linux.py", Function: "connect", LineNo: 42, InApp: &inApp},
			{Filename: "lib.py", Function: "helper", LineNo: 7},
		}
		cmd := pkg.BuildCreateCommand(withTrace, cfg)
		gomega.Expect(cmd.Body).
			To(gomega.ContainSubstring("- Exception: ConnectionRefusedError: refused"))
		gomega.Expect(cmd.Body).To(gomega.ContainSubstring("- Module: mt5.connector.mt5linux"))
		gomega.Expect(cmd.Body).To(gomega.ContainSubstring("- Trace:"))
		gomega.Expect(cmd.Body).
			To(gomega.ContainSubstring("- mt5linux.py:42 in connect (in_app=1)"))
		gomega.Expect(cmd.Body).To(gomega.ContainSubstring("- lib.py:7 in helper (in_app=unknown)"))
	})

	ginkgo.It("omits the exception block when the issue has none", func() {
		cmd := pkg.BuildCreateCommand(issue, cfg)
		gomega.Expect(cmd.Body).To(gomega.Not(gomega.ContainSubstring("- Exception:")))
		gomega.Expect(cmd.Body).To(gomega.Not(gomega.ContainSubstring("- Trace:")))
	})

	ginkgo.It("caps the trace at five frames", func() {
		withTrace := issue
		withTrace.ExceptionType = "RuntimeError"
		withTrace.ExceptionValue = "boom"
		var frames []pkg.Frame
		for i := 0; i < 8; i++ {
			frames = append(frames, pkg.Frame{Filename: "f.go", Function: "fn", LineNo: i})
		}
		withTrace.Trace = frames
		cmd := pkg.BuildCreateCommand(withTrace, cfg)
		gomega.Expect(strings.Count(cmd.Body, "\n  - ")).To(gomega.Equal(5))
	})

	ginkgo.It("derives a deterministic task identifier from the kafka key", func() {
		cmd1 := pkg.BuildCreateCommand(issue, cfg)
		cmd2 := pkg.BuildCreateCommand(issue, cfg)
		gomega.Expect(cmd1.TaskIdentifier).To(gomega.Equal(cmd2.TaskIdentifier))
		// distinct from the collector's sentry: identity space
		gomega.Expect(string(cmd1.TaskIdentifier)).
			To(gomega.Not(gomega.ContainSubstring("sentry:")))
	})

	ginkgo.It("derives a fresh task per date", func() {
		other := issue
		other.Date = "2026-09-10"
		cmd1 := pkg.BuildCreateCommand(issue, cfg)
		cmd2 := pkg.BuildCreateCommand(other, cfg)
		gomega.Expect(cmd1.TaskIdentifier).To(gomega.Not(gomega.Equal(cmd2.TaskIdentifier)))
	})
})
