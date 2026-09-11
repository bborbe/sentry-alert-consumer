// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("BuildIssue", func() {
	ctx := context.Background()
	rec := pkg.Record{
		Body:       `{"event_id":"abc123"}`,
		Project:    "nuke-prod",
		ReceivedAt: "2026-09-09T20:10:00Z",
		Outcome:    "rejected",
	}

	ginkgo.It("derives a stable key from the first exception", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","exception":{"values":[{"type":"RuntimeError","value":"boom","module":"mymod"}]}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		// SHA-256("mymod:RuntimeError:boom") truncated to 32 hex chars.
		gomega.Expect(issue.Key).To(gomega.Equal("01d804fbb7f4b03070a1d12d66b17430"))
		gomega.Expect(issue.Title).To(gomega.Equal("boom"))
		gomega.Expect(issue.Date).To(gomega.Equal("2026-09-09"))
		gomega.Expect(issue.Outcome).To(gomega.Equal("rejected"))
	})

	ginkgo.It("carries the exception snapshot for the derived-key task body", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","exception":{"values":[{"type":"ConnectionRefusedError","value":"refused","module":"mt5.connector","stacktrace":{"frames":[{"filename":"mt5linux.py","function":"connect","lineno":42,"in_app":true}]}}]}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.ExceptionType).To(gomega.Equal("ConnectionRefusedError"))
		gomega.Expect(issue.ExceptionValue).To(gomega.Equal("refused"))
		gomega.Expect(issue.ExceptionModule).To(gomega.Equal("mt5.connector"))
		gomega.Expect(issue.Trace).To(gomega.HaveLen(1))
		gomega.Expect(issue.Trace[0].Filename).To(gomega.Equal("mt5linux.py"))
		gomega.Expect(issue.Trace[0].LineNo).To(gomega.Equal(42))
		gomega.Expect(issue.Trace[0].InApp).To(gomega.Not(gomega.BeNil()))
		gomega.Expect(*issue.Trace[0].InApp).To(gomega.BeTrue())
	})

	ginkgo.It("leaves the exception snapshot empty without an exception", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","message":{"formatted":"boom happened"}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.ExceptionType).To(gomega.Equal(""))
		gomega.Expect(issue.ExceptionValue).To(gomega.Equal(""))
		gomega.Expect(issue.ExceptionModule).To(gomega.Equal(""))
		gomega.Expect(issue.Trace).To(gomega.BeEmpty())
	})

	ginkgo.It("groups identical exceptions into the same key", func() {
		body := `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","exception":{"values":[{"type":"RuntimeError","value":"boom","module":"mymod"}]}}`
		env1, _ := pkg.ParseEnvelope(ctx, body)
		env2, _ := pkg.ParseEnvelope(ctx, body)
		i1, _ := pkg.BuildIssue(ctx, env1, rec)
		i2, _ := pkg.BuildIssue(ctx, env2, rec)
		gomega.Expect(i1.Key).To(gomega.Equal(i2.Key))
	})

	ginkgo.It("falls back to the message when no exception is present", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","message":{"formatted":"boom happened"}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.Key).To(gomega.Not(gomega.Equal("")))
		gomega.Expect(issue.Title).To(gomega.Equal("boom happened"))
	})

	ginkgo.It("falls back to a per-event identity when only event_id exists", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z"}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.Key).To(gomega.Equal("event-abc123"))
	})

	ginkgo.It("falls back to a body hash for an unparseable envelope", func() {
		issue, err := pkg.BuildIssue(ctx, pkg.Envelope{}, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.Key).To(gomega.Not(gomega.Equal("")))
		gomega.Expect(issue.Title).To(gomega.Equal("(no message)"))
	})

	ginkgo.It("derives stage from the project slug", func() {
		gomega.Expect(pkg.DeriveStage("nuke-prod", "dev")).To(gomega.Equal("prod"))
		gomega.Expect(pkg.DeriveStage("nuke-dev", "dev")).To(gomega.Equal("dev"))
		gomega.Expect(pkg.DeriveStage("other-project", "dev")).To(gomega.Equal("dev"))
	})

	ginkgo.It("errors on a missing timestamp", func() {
		recNoTime := rec
		recNoTime.ReceivedAt = ""
		_, err := pkg.BuildIssue(ctx, pkg.Envelope{}, recNoTime)
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("never derives a key with the title-forbidden colon", func() {
		// CreateTaskCommand title validation rejects ':' — a colon anywhere in
		// the issue key would kill every publish (observed 2026-09-10 on the
		// "event:<id>" per-event fallback). Every fallback path must stay
		// colon-free.
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","exception":{"values":[{"type":"RuntimeError","value":"boom","module":"mymod"}]}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		issue, err := pkg.BuildIssue(ctx, env, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(issue.Key).To(gomega.Not(gomega.ContainSubstring(":")))

		perEvent, err := pkg.BuildIssue(ctx, pkg.Envelope{EventID: "abc123"}, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(perEvent.Key).To(gomega.Not(gomega.ContainSubstring(":")))

		bodyFallback, err := pkg.BuildIssue(ctx, pkg.Envelope{}, rec)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(bodyFallback.Key).To(gomega.Not(gomega.ContainSubstring(":")))
	})
})
