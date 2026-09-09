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

var _ = ginkgo.Describe("ParseRecord", func() {
	ctx := context.Background()

	ginkgo.It("decodes a valid tee record", func() {
		rec, err := pkg.ParseRecord(ctx, []byte(`{
			"body": "{\"event_id\":\"abc\"}",
			"project": "nuke-prod",
			"received_at": "2026-09-09T20:10:00Z",
			"outcome": "rejected"
		}`))
		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(rec.Body).To(gomega.Equal(`{"event_id":"abc"}`))
		gomega.Expect(rec.Project).To(gomega.Equal("nuke-prod"))
		gomega.Expect(rec.ReceivedAt).To(gomega.Equal("2026-09-09T20:10:00Z"))
		gomega.Expect(rec.Outcome).To(gomega.Equal("rejected"))
	})

	ginkgo.It("rejects invalid JSON", func() {
		_, err := pkg.ParseRecord(ctx, []byte(`not-json`))
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("rejects a record with an empty body", func() {
		_, err := pkg.ParseRecord(ctx, []byte(`{
			"body": "",
			"project": "nuke-prod",
			"received_at": "2026-09-09T20:10:00Z",
			"outcome": "forwarded"
		}`))
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("rejects a record without received_at", func() {
		_, err := pkg.ParseRecord(ctx, []byte(`{
			"body": "{}",
			"project": "nuke-prod",
			"outcome": "forwarded"
		}`))
		gomega.Expect(err).To(gomega.HaveOccurred())
	})
})
