// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import "github.com/prometheus/client_golang/prometheus"

//counterfeiter:generate -o ../mocks/metrics.go --fake-name Metrics . Metrics

// Metrics is the observable counter surface required by [[Watcher Writing Guide]]
// § Required observability.
type Metrics interface {
	// IncPollCycle — result: "success" | "sentry_error"
	IncPollCycle(result string)

	// IncPublished — status: "create" | "error"
	IncPublished(status string)

	// IncFilterSkipped — reason: "project_not_in_scope" | "already_seen" |
	// "missing_short_id" | "invalid_date" | "build_error"
	IncFilterSkipped(reason string)

	// IncIssuesScanned — sanity check on the per-cycle scan size.
	IncIssuesScanned(count int)
}

// metricNamespace is the single Prometheus metric namespace for this binary
// (must match the binary name per [[Watcher Writing Guide]]). A lone default,
// not a member of a closed enum set, so it stays an untyped string.
const metricNamespace = "sentry_watcher"

// NewMetrics returns the Prometheus-backed Metrics implementation registered
// against the supplied Registerer. Pass nil for the default registry.
// Pre-initialises every label combination so Prometheus exposes a zero series
// before the first event fires.
func NewMetrics(registerer prometheus.Registerer) Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	m := &prometheusMetrics{
		pollCycleTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "poll_cycle_total",
			Help:      "Total poll cycles by result.",
		}, []string{"result"}),
		publishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "published_total",
			Help:      "Total task-publish attempts by status.",
		}, []string{"status"}),
		filterSkippedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "filter_skipped_total",
			Help:      "Total issues filtered out by reason.",
		}, []string{"reason"}),
		issuesScannedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "issues_scanned_total",
			Help:      "Total Sentry issues scanned across poll cycles.",
		}),
	}
	registerer.MustRegister(
		m.pollCycleTotal,
		m.publishedTotal,
		m.filterSkippedTotal,
		m.issuesScannedTotal,
	)
	for _, r := range []string{"success", "sentry_error"} {
		m.pollCycleTotal.WithLabelValues(r).Add(0)
	}
	for _, s := range []string{"create", "error"} {
		m.publishedTotal.WithLabelValues(s).Add(0)
	}
	for _, r := range []string{
		"project_not_in_scope", "already_seen", "missing_short_id", "invalid_date", "build_error",
	} {
		m.filterSkippedTotal.WithLabelValues(r).Add(0)
	}
	return m
}

type prometheusMetrics struct {
	pollCycleTotal     *prometheus.CounterVec
	publishedTotal     *prometheus.CounterVec
	filterSkippedTotal *prometheus.CounterVec
	issuesScannedTotal prometheus.Counter
}

func (m *prometheusMetrics) IncPollCycle(result string) {
	m.pollCycleTotal.WithLabelValues(result).Inc()
}

func (m *prometheusMetrics) IncPublished(status string) {
	m.publishedTotal.WithLabelValues(status).Inc()
}

func (m *prometheusMetrics) IncFilterSkipped(reason string) {
	m.filterSkippedTotal.WithLabelValues(reason).Inc()
}

func (m *prometheusMetrics) IncIssuesScanned(count int) {
	m.issuesScannedTotal.Add(float64(count))
}
