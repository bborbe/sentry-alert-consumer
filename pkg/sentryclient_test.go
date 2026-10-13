// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
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

	// This table is about the wire shape, not the retry schedule, and it runs
	// the real waiter — so it passes an event already past the indexing window
	// deliberately: the 404 entry then stays a single call instead of sleeping
	// the schedule's 10 s. The schedule itself is covered under "SentryClient
	// retry", where the waiter is injected.
	staleEventTime := time.Now().Add(-5 * time.Minute)

	ginkgo.DescribeTable("IssueRef",
		func(status int, body string, assert func(baseURL string, ref pkg.IssueRef, err error)) {
			var got request
			server := newServer(status, body, &got)
			defer server.Close()

			client := pkg.NewSentryClient(server.URL, "myorg", "secret-token")
			ref, err := client.IssueRef(ctx, "abc-123", staleEventTime)

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
		ref, err := client.IssueRef(ctx, "abc-123", staleEventTime)

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
		_, err := client.IssueRef(ctx, "a b/c", staleEventTime)

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
		ref, err := client.IssueRef(cancelled, "abc-123", staleEventTime)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
		gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
	})
})

var _ = ginkgo.Describe("SentryClient retry", func() {
	ctx := context.Background()

	// newSequenceServer replies with statuses in order (the last one repeats
	// once the sequence is exhausted) and counts every call, so each spec can
	// assert exactly how many lookups were issued.
	newSequenceServer := func(calls *int, retryAfter string, statuses ...int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			status := statuses[len(statuses)-1]
			if *calls < len(statuses) {
				status = statuses[*calls]
			}
			*calls++
			if status != http.StatusOK && retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = w.Write([]byte(`{"event":{"groupID":"42"}}`))
			}
		}))
	}

	// newRecordingWaiter captures each backoff instead of sleeping, so the
	// schedule is asserted directly and the suite never waits in real time.
	newRecordingWaiter := func(waits *[]time.Duration) libtime.WaiterDuration {
		return libtime.WaiterDurationFunc(func(_ context.Context, d libtime.Duration) error {
			*waits = append(*waits, d.Duration())
			return nil
		})
	}

	// The 404 schedule is gated on the event's own age, so every spec states
	// which side of the indexing window it exercises. freshEventTime is what a
	// just-forwarded event looks like; staleEventTime is what a record replayed
	// from the topic days later looks like.
	freshEventTime := time.Now()
	staleEventTime := time.Now().Add(-5 * time.Minute)

	totalWait := func(waits []time.Duration) time.Duration {
		var total time.Duration
		for _, w := range waits {
			total += w
		}
		return total
	}

	ginkgo.It("retries a 429 and enriches from the follow-up 200", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusTooManyRequests, http.StatusOK)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(ref.NumericID).To(gomega.Equal("42"))
		gomega.Expect(calls).To(gomega.Equal(2))
		gomega.Expect(waits).To(gomega.Equal([]time.Duration{100 * time.Millisecond}))
	})

	ginkgo.It("applies the Retry-After delay Sentry supplies", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "2", http.StatusTooManyRequests, http.StatusOK)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(ref.NumericID).To(gomega.Equal("42"))
		// The header wins over the 100ms exponential default.
		gomega.Expect(waits).To(gomega.Equal([]time.Duration{2 * time.Second}))
	})

	ginkgo.It("falls back to exponential backoff when no Retry-After is sent", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusTooManyRequests)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		_, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(waits).To(gomega.Equal([]time.Duration{
			100 * time.Millisecond,
			200 * time.Millisecond,
		}))
	})

	ginkgo.It("gives up after three 429s and reports retries exhausted", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusTooManyRequests)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.Is(err, pkg.ErrRetriesExhausted)).To(gomega.BeTrue())
		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
		gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
		gomega.Expect(calls).To(gomega.Equal(3))
		// Total waiting stays inside the 2s budget.
		var total time.Duration
		for _, w := range waits {
			total += w
		}
		gomega.Expect(total).To(gomega.BeNumerically("<=", 2*time.Second))
	})

	ginkgo.It("abandons the retry rather than honour a Retry-After beyond the budget", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "60", http.StatusServiceUnavailable)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		_, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(errors.Is(err, pkg.ErrRetriesExhausted)).To(gomega.BeTrue())
		gomega.Expect(calls).To(gomega.Equal(1))
		gomega.Expect(waits).To(gomega.BeEmpty())
	})

	// A 500 stays terminal whatever the event's age: some APIs return 500 for a
	// missing resource, so retrying it would burn Sentry quota on a permanent
	// miss. A 404 is terminal only once the event is past the indexing window —
	// see the schedule specs below.
	ginkgo.It("does not retry a 500", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusInternalServerError)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		_, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.Is(err, pkg.ErrRetriesExhausted)).To(gomega.BeFalse())
		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeFalse())
		gomega.Expect(calls).To(gomega.Equal(1))
		gomega.Expect(waits).To(gomega.BeEmpty())
	})

	ginkgo.It("skips the 404 schedule for an event past the indexing window", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusNotFound)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", staleEventTime)

		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeTrue())
		gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
		// Exactly one lookup and no wait: an event replayed from the topic has
		// a real miss, and holding the partition 10 s per record would stall a
		// replay burst of 45 misses for minutes.
		gomega.Expect(calls).To(gomega.Equal(1))
		gomega.Expect(waits).To(gomega.BeEmpty())
	})

	ginkgo.It("retries a 404 on a fresh event and enriches from the follow-up 200", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(
			&calls, "", http.StatusNotFound, http.StatusNotFound, http.StatusOK,
		)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(err).To(gomega.Not(gomega.HaveOccurred()))
		gomega.Expect(ref.NumericID).To(gomega.Equal("42"))
		gomega.Expect(calls).To(gomega.Equal(3))
		// The offsets are absolute from the first attempt, so the waits are the
		// gaps between them: 1s to the second attempt, then 2s more to the
		// third.
		gomega.Expect(waits).To(gomega.Equal([]time.Duration{
			1 * time.Second,
			2 * time.Second,
		}))
		gomega.Expect(totalWait(waits)).To(gomega.BeNumerically("<=", 10*time.Second))
	})

	ginkgo.It("gives up after the full 404 schedule on a fresh event", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusNotFound)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		ref, err := client.IssueRef(ctx, "abc-123", freshEventTime)

		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeTrue())
		gomega.Expect(errors.Is(err, pkg.ErrRetriesExhausted)).To(gomega.BeFalse())
		gomega.Expect(ref).To(gomega.Equal(pkg.IssueRef{}))
		// One error-retry attempt plus the four schedule attempts.
		gomega.Expect(calls).To(gomega.Equal(5))
		gomega.Expect(waits).To(gomega.Equal([]time.Duration{
			1 * time.Second,
			2 * time.Second,
			3 * time.Second,
			4 * time.Second,
		}))
		gomega.Expect(totalWait(waits)).To(gomega.BeNumerically("<=", 10*time.Second))
	})

	ginkgo.It("treats a zero event time as old and skips the 404 schedule", func() {
		var calls int
		var waits []time.Duration
		server := newSequenceServer(&calls, "", http.StatusNotFound)
		defer server.Close()

		client := pkg.NewSentryClientWithWaiter(
			server.URL, "myorg", "secret-token", newRecordingWaiter(&waits),
		)
		// A zero time is what an unparseable timestamp resolves to, so the
		// conservative direction is asserted rather than left to chance: the
		// schedule is skipped rather than spent on an age we cannot establish.
		_, err := client.IssueRef(ctx, "abc-123", time.Time{})

		gomega.Expect(errors.Is(err, pkg.ErrEventNotFound)).To(gomega.BeTrue())
		gomega.Expect(calls).To(gomega.Equal(1))
		gomega.Expect(waits).To(gomega.BeEmpty())
	})
})
