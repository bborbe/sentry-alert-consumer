// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/bborbe/errors"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
	"github.com/bborbe/sentry-alert-consumer/pkg/factory"
)

var _ = ginkgo.Describe("CreateSentryClient", func() {
	ctx := context.Background()

	// The token-less client must resolve nothing and never touch the network:
	// the binary runs without SENTRY_API_TOKEN and every task keeps its
	// derived key.
	ginkgo.DescribeTable("disables enrichment without a token",
		func(token string) {
			client := factory.CreateSentryClient("https://bborbe.sentry.io", "bborbe", token)
			ref, err := client.IssueRef(ctx, "abc-123")
			gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeTrue())
			gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
		},
		ginkgo.Entry("empty token", ""),
		ginkgo.Entry("whitespace token", "   "),
	)

	ginkgo.It("resolves the event when a token is set", func() {
		var auth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"event":{"groupID":"9"}}`))
		}))
		defer server.Close()

		client := factory.CreateSentryClient(server.URL, "bborbe", "tok")
		ref, err := client.IssueRef(ctx, "abc-123")

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(ref.NumericID).To(gomega.Equal("9"))
		gomega.Expect(auth).To(gomega.Equal("Bearer tok"))
	})
})
