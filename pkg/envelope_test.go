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

	ginkgo.It("accepts the bare-array exception form sentry-go emits", func() {
		// sentry-go serialises Exception []Exception with json tag "exception",
		// so bborbe Go services put "exception":[{...}] on the topic — not the
		// protocol's {"values":[...]}. Before this was accepted the payload
		// failed to unmarshal, Event stayed nil, and every event fell through to
		// the per-event identity fallback (2026-09-21/22 flood).
		env, err := pkg.ParseEnvelope(
			ctx,
			`{"event_id":"abc123","sent_at":"2026-09-23T16:00:33Z","sdk":{"name":"sentry.go","version":"0.49.0"}}
{"type":"event","length":11246}
{"event_id":"abc123","level":"error","platform":"go","exception":[{"type":"*errors.errorString","value":"kafka: broker not connected","mechanism":{"type":"chained","source":"unwrap"}}]}`,
		)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(env.Event).To(gomega.Not(gomega.BeNil()))
		gomega.Expect(env.Event.Exception.Values).To(gomega.HaveLen(1))
		gomega.Expect(env.Event.Exception.Values[0].Type).To(gomega.Equal("*errors.errorString"))
		gomega.Expect(env.Event.Exception.Values[0].Value).
			To(gomega.Equal("kafka: broker not connected"))
	})

	ginkgo.It("extracts the exception stacktrace frames, preserving unknown in_app", func() {
		env, err := pkg.ParseEnvelope(ctx, `{"event_id":"abc123"}
{"type":"event"}
{"event_id":"abc123","exception":{"values":[{"type":"ConnectionRefusedError","value":"refused","module":"m","stacktrace":{"frames":[{"filename":"mt5linux.py","function":"connect","lineno":42,"in_app":true},{"filename":"lib.py","function":"helper","lineno":7,"in_app":false},{"filename":"vendor.py","function":"x"}]}}]}}`)
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		v := env.Event.Exception.Values[0]
		gomega.Expect(v.Stacktrace.Frames).To(gomega.HaveLen(3))
		gomega.Expect(v.Stacktrace.Frames[0].Filename).To(gomega.Equal("mt5linux.py"))
		gomega.Expect(v.Stacktrace.Frames[0].LineNo).To(gomega.Equal(42))
		gomega.Expect(v.Stacktrace.Frames[0].InApp).To(gomega.Not(gomega.BeNil()))
		gomega.Expect(*v.Stacktrace.Frames[0].InApp).To(gomega.BeTrue())
		gomega.Expect(*v.Stacktrace.Frames[1].InApp).To(gomega.BeFalse())
		// absent in_app → nil (unknown preserved, not collapsed to false)
		gomega.Expect(v.Stacktrace.Frames[2].InApp).To(gomega.BeNil())
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
