// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/bborbe/errors"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("SentryClient", func() {
	ctx := context.Background()

	// request is one captured outbound call, so every branch asserts the
	// documented wire shape (method, path, bearer token) alongside the result.
	type request struct {
		method string
		path   string
		auth   string
	}

	newServer := func(status int, body string, got *request) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*got = request{
				method: r.Method,
				path:   r.URL.EscapedPath(),
				auth:   r.Header.Get("Authorization"),
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	ginkgo.DescribeTable("IssueRef",
		func(status int, body string, assert func(baseURL string, ref pkg.IssueRef, err error)) {
			var got request
			server := newServer(status, body, &got)
			defer server.Close()

			client := pkg.NewSentryClient(server.URL, "myorg", "secret-token")
			ref, err := client.IssueRef(ctx, "abc-123")

			gomega.Expect(got.method).To(gomega.Equal(http.MethodGet))
			gomega.Expect(got.path).
				To(gomega.Equal("/api/0/organizations/myorg/eventids/abc-123/"))
			gomega.Expect(got.auth).To(gomega.Equal("Bearer secret-token"))
			assert(server.URL, ref, err)
		},
		ginkgo.Entry(
			"resolves the numeric issue id and its url on 200",
			http.StatusOK,
			`{"event":{"groupID":"1","eventID":"abc-123"},"projectSlug":"nuke-prod"}`,
			func(baseURL string, ref pkg.IssueRef, err error) {
				gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
				gomega.Expect(ref.NumericID).To(gomega.Equal("1"))
				gomega.Expect(ref.URL).To(gomega.Equal(baseURL + "/issues/1/"))
			},
		),
		ginkgo.Entry(
			"reports an unknown event as ErrEventNotFound on 404",
			http.StatusNotFound,
			`{"detail":"Event not found"}`,
			func(_ string, ref pkg.IssueRef, err error) {
				gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeTrue())
				gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
			},
		),
		ginkgo.Entry(
			"wraps an unexpected status as a plain error on 500",
			http.StatusInternalServerError,
			`{"detail":"internal error"}`,
			func(_ string, ref pkg.IssueRef, err error) {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("500"))
				gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
			},
		),
		ginkgo.Entry(
			"wraps a malformed json body as an error",
			http.StatusOK,
			`{"event":{"groupID":`,
			func(_ string, ref pkg.IssueRef, err error) {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
				gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
			},
		),
		ginkgo.Entry(
			"wraps a 200 without a numeric issue id as an error",
			http.StatusOK,
			`{"projectSlug":"nuke-prod"}`,
			func(_ string, ref pkg.IssueRef, err error) {
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("groupID"))
				gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
			},
		),
	)

	ginkgo.It("tolerates a trailing slash on the base url", func() {
		var got request
		server := newServer(http.StatusOK, `{"event":{"groupID":"7"}}`, &got)
		defer server.Close()

		client := pkg.NewSentryClient(server.URL+"/", "myorg", "secret-token")
		ref, err := client.IssueRef(ctx, "abc-123")

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(got.path).
			To(gomega.Equal("/api/0/organizations/myorg/eventids/abc-123/"))
		gomega.Expect(ref.URL).To(gomega.Equal(server.URL + "/issues/7/"))
	})

	ginkgo.It("escapes the event id in the lookup path", func() {
		var got request
		server := newServer(http.StatusOK, `{"event":{"groupID":"7"}}`, &got)
		defer server.Close()

		client := pkg.NewSentryClient(server.URL, "myorg", "secret-token")
		_, err := client.IssueRef(ctx, "a b/c")

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(got.path).
			To(gomega.Equal("/api/0/organizations/myorg/eventids/a%20b%2Fc/"))
	})

	ginkgo.It("honours context cancellation", func() {
		var got request
		server := newServer(http.StatusOK, `{"event":{"groupID":"7"}}`, &got)
		defer server.Close()

		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		client := pkg.NewSentryClient(server.URL, "myorg", "secret-token")
		ref, err := client.IssueRef(cancelled, "abc-123")

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
		gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
	})
})
