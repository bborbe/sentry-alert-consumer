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

var _ = ginkgo.Describe("parseEnvelope", func() {
	ctx := context.Background()

	ginkgo.It("extracts event_id and the event payload", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123","sent_at":"2026-09-09T20:10:00Z"}
{"type":"event","length":123}
{"event_id":"abc123","timestamp":"2026-09-09T20:09:00Z","level":"error","message":{"formatted":"boom happened"},"exception":{"values":[{"type":"RuntimeError","value":"boom","module":"mymod"}]}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(env.EventID).To(gomega.Equal("abc123"))
		gomega.Expect(env.Event).To(gomega.Not(gomega.BeNil()))
		gomega.Expect(env.Event.Exception.Values).To(gomega.HaveLen(1))
		gomega.Expect(env.Event.Exception.Values[0].Type).To(gomega.Equal("RuntimeError"))
		gomega.Expect(env.Event.Message.Formatted).To(gomega.Equal("boom happened"))
	})

	ginkgo.It("degrades to the header identity when the event payload is missing", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event","length":5}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(env.EventID).To(gomega.Equal("abc123"))
		gomega.Expect(env.Event).To(gomega.BeNil())
	})

	ginkgo.It("degrades to the header identity when the event payload is malformed", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
not-json`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(env.EventID).To(gomega.Equal("abc123"))
		gomega.Expect(env.Event).To(gomega.BeNil())
	})

	ginkgo.It("errors on an unparseable header line", func() {
		_, err := pkg.ParseEnvelope(ctx, `not-json-header`)
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("errors on an empty envelope", func() {
		_, err := pkg.ParseEnvelope(ctx, "")
		gomega.Expect(err).To(gomega.HaveOccurred())
	})
})
