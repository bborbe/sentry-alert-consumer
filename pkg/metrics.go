// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

//counterfeiter:generate -o ../mocks/metrics.go --fake-name Metrics . Metrics

// Metrics is the observable counter surface of the consumer.
type Metrics interface {
	// IncMessages — result: "processed" | "parse_error" | "envelope_error" |
	// "build_error" | "publish_error" | "enrich_error" |
	// "enrich_retry_exhausted" | "skipped_heartbeat"
	//
	// "enrich_error" is a single non-retryable enrichment failure;
	// "enrich_retry_exhausted" means the client retried and gave up, i.e.
	// Sentry is sustainedly rate-limiting or down. Only the latter is worth
	// alerting on.
	IncMessages(result string)

	// IncEvents — outcome: "forwarded" | "rejected" | "upstream_error"
	IncEvents(outcome string)

	// SetLastEvent records the received_at (RFC3339) of the last processed
	// event as a Unix timestamp — the silence signal an alert thresholds on.
	SetLastEvent(receivedAt string)

	// IncPublished — status: "create" | "error" (task publish attempts)
	IncPublished(status string)

	// SetKafkaLag records the consumer's own lag (broker highwater minus the
	// highest consumed offset) for a partition — the broken-consumer signal
	// an alert thresholds on. Sourced from this consumer, not from the
	// strimzi-topic-reader's per-read cache.
	SetKafkaLag(partition int32, lag float64)
}

// metricNamespace is the single Prometheus metric namespace for this binary.
const metricNamespace = "sentry_alert_consumer"

// Int64Formatter formats an integer for use as a Prometheus label value.
// Injected so the metrics adapter has no hidden strconv dependency.
type Int64Formatter interface {
	Format(value int64) string
}

type strconvInt64Formatter struct{}

func (strconvInt64Formatter) Format(value int64) string {
	return strconv.FormatInt(value, 10)
}

// NewMetrics returns the Prometheus-backed Metrics implementation registered
// against the supplied Registerer. Pass nil for the default registry.
// Pre-initialises every label combination so Prometheus exposes a zero series
// before the first event fires.
func NewMetrics(registerer prometheus.Registerer, formatter Int64Formatter) Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	if formatter == nil {
		formatter = strconvInt64Formatter{}
	}
	m := &prometheusMetrics{
		messagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "messages_total",
			Help:      "Total topic messages processed by result.",
		}, []string{"result"}),
		eventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "events_total",
			Help:      "Total events by tee outcome.",
		}, []string{"outcome"}),
		lastEventTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "last_event_timestamp",
			Help:      "Unix timestamp of the last processed event (0 = none since start).",
		}),
		publishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "published_total",
			Help:      "Total task-publish attempts by status.",
		}, []string{"status"}),
		kafkaLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "kafka_lag",
			Help:      "Messages behind the broker highwater per partition (0 = caught up).",
		}, []string{"partition"}),
		formatter: formatter,
	}
	registerer.MustRegister(
		m.messagesTotal,
		m.eventsTotal,
		m.lastEventTimestamp,
		m.publishedTotal,
		m.kafkaLag,
	)
	for _, r := range []string{
		"processed", "parse_error", "envelope_error", "build_error", "publish_error",
		"enrich_error", "enrich_retry_exhausted", "skipped_heartbeat",
	} {
		m.messagesTotal.WithLabelValues(r).Add(0)
	}
	for _, o := range []string{"forwarded", "rejected", "upstream_error"} {
		m.eventsTotal.WithLabelValues(o).Add(0)
	}
	for _, s := range []string{"create", "error"} {
		m.publishedTotal.WithLabelValues(s).Add(0)
	}
	return m
}

type prometheusMetrics struct {
	messagesTotal      *prometheus.CounterVec
	eventsTotal        *prometheus.CounterVec
	lastEventTimestamp prometheus.Gauge
	publishedTotal     *prometheus.CounterVec
	kafkaLag           *prometheus.GaugeVec
	formatter          Int64Formatter
}

func (m *prometheusMetrics) IncMessages(result string) {
	m.messagesTotal.WithLabelValues(result).Inc()
}

func (m *prometheusMetrics) IncEvents(outcome string) {
	m.eventsTotal.WithLabelValues(outcome).Inc()
}

func (m *prometheusMetrics) SetLastEvent(receivedAt string) {
	if t, err := time.Parse(time.RFC3339, receivedAt); err == nil {
		m.lastEventTimestamp.Set(float64(t.Unix()))
	}
}

func (m *prometheusMetrics) IncPublished(status string) {
	m.publishedTotal.WithLabelValues(status).Inc()
}

func (m *prometheusMetrics) SetKafkaLag(partition int32, lag float64) {
	m.kafkaLag.WithLabelValues(m.formatter.Format(int64(partition))).Set(lag)
}
