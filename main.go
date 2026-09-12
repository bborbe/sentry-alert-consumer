// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command sentry-alert-consumer consumes the {branch}-raw-sentry-alert-input
// Kafka topic and publishes one CreateTaskCommand per derived (issue-key,
// date) so the sentry-analyzer-agent can classify each alert — replacing the
// sentry-collector-agent's token-REST fan-out as the trigger of the Sentry
// pipeline.
//
// See the task "Analyse Sentry Alerts Directly From the Kafka Topic" §
// Consumer Contract for the full contract (envelope-derived grouping,
// silence-window, enrichment-only Sentry API).
package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bborbe/sentry-alert-consumer/pkg"
	"github.com/bborbe/sentry-alert-consumer/pkg/factory"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// SentryAPIToken enables the event→issue enrichment. display:"length" is
	// mandatory: without it argument.Parse() prints the token value to stdout
	// at startup. Empty disables enrichment (tasks keep the derived key).
	SentryAPIToken string `required:"false" arg:"sentry-api-token" env:"SENTRY_API_TOKEN" usage:"Sentry API token for event→issue enrichment"         display:"length"`
	SentryURL      string `required:"false" arg:"sentry-url"       env:"SENTRY_URL"       usage:"Sentry base URL for event→issue enrichment"                           default:"https://bborbe.sentry.io"`
	SentryOrg      string `required:"false" arg:"sentry-org"       env:"SENTRY_ORG"       usage:"Sentry organization slug for event→issue enrichment"                  default:"bborbe"`

	Listen       string           `required:"false" arg:"listen"        env:"LISTEN"        usage:"HTTP listen address (healthz/readiness/metrics)"              default:":9090"`
	Stage        string           `required:"true"  arg:"stage"         env:"STAGE"         usage:"Deployment stage (dev|prod)"                                  default:"dev"`
	KafkaBrokers libkafka.Brokers `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma-separated Kafka broker list"`
	KafkaTopic   string           `required:"true"  arg:"kafka-topic"   env:"KAFKA_TOPIC"   usage:"Alert topic to consume (e.g. develop-raw-sentry-alert-input)"`

	// TopicPrefix selects the Kafka topic prefix used for CQRS command topic
	// construction (e.g. "develop" / "master"); empty means unprefixed topics.
	TopicPrefix base.TopicPrefix `required:"false" arg:"topic-prefix" env:"TOPIC_PREFIX" usage:"Kafka topic prefix for CQRS topic construction"`

	TaskAssignee string `required:"false" arg:"task-assignee" env:"TASK_ASSIGNEE" usage:"Frontmatter assignee for published sentry-issue-analyzer tasks"     default:"sentry-analyzer-agent"`
	TaskStatus   string `required:"false" arg:"task-status"   env:"TASK_STATUS"   usage:"Frontmatter status for published sentry-issue-analyzer tasks"       default:"in_progress"`
	TaskPhase    string `required:"false" arg:"task-phase"    env:"TASK_PHASE"    usage:"Frontmatter phase for published sentry-issue-analyzer tasks"        default:"planning"`
	TargetVault  string `required:"false" arg:"target-vault"  env:"TARGET_VAULT"  usage:"Obsidian vault slug the task-controller materializes the task into" default:"personal"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	if strings.TrimSpace(a.KafkaTopic) == "" {
		return errors.Errorf(ctx, "KAFKA_TOPIC must be set (e.g. develop-raw-sentry-alert-input)")
	}

	metrics := pkg.NewMetrics(nil, nil)

	syncProducer, err := libkafka.NewSyncProducerWithName(
		ctx,
		a.KafkaBrokers,
		"sentry-alert-consumer",
	)
	if err != nil {
		return errors.Wrap(ctx, err, "create sync producer")
	}
	defer func() {
		if cerr := syncProducer.Close(); cerr != nil {
			glog.Warningf("close kafka sync producer: %v", cerr)
		}
	}()

	saramaClient, err := libkafka.CreateSaramaClient(ctx, a.KafkaBrokers)
	if err != nil {
		return errors.Wrap(ctx, err, "create sarama client")
	}
	defer func() {
		if cerr := saramaClient.Close(); cerr != nil {
			glog.Warningf("close sarama client: %v", cerr)
		}
	}()

	sender := factory.CreateKafkaSender(syncProducer, a.TopicPrefix, a.TargetVault)
	sentryClient := factory.CreateSentryClient(a.SentryURL, a.SentryOrg, a.SentryAPIToken)
	consumer := factory.CreateConsumer(sender, metrics, pkg.TaskConfig{
		Stage:    a.Stage,
		Assignee: a.TaskAssignee,
		Status:   a.TaskStatus,
		Phase:    a.TaskPhase,
	}, sentryClient)

	// Start from the oldest offset so no event is missed on first start; the
	// simple offset manager is in-memory, so a restart replays from the
	// beginning — safe because controller dedup (UUID5 over kafka:key:date)
	// makes re-emits a no-op.
	loop := libkafka.NewSimpleConsumer(
		saramaClient,
		libkafka.Topic(a.KafkaTopic),
		libkafka.OffsetOldest,
		libkafka.MessageHandlerFunc(consumer.ConsumeMessage),
		log.DefaultSamplerFactory,
	)

	// The lag tracker polls the broker highwater on a fixed cadence and exports
	// this consumer's own lag — the broken-consumer signal the lag alert
	// thresholds on. Sourced from this consumer's position, not the
	// strimzi-topic-reader's per-read cache.
	lagTracker := pkg.NewLagTracker(
		saramaClient,
		a.KafkaTopic,
		consumer,
		metrics,
		tickerAdapter{ticker: time.NewTicker(15 * time.Second)},
		glogLogger{},
	)

	glog.V(2).Infof(
		"sentry-alert-consumer starting stage=%s topic=%s listen=%s",
		a.Stage, a.KafkaTopic, a.Listen,
	)

	return run.CancelOnFirstFinish(ctx,
		run.Func(loop.Consume),
		run.Func(lagTracker.Run),
		a.createHTTPServer(),
	)
}

// tickerAdapter adapts a time.Ticker to the pkg.Ticker interface so the
// lag tracker stays clock-free.
type tickerAdapter struct {
	ticker *time.Ticker
}

func (a tickerAdapter) Chan() <-chan time.Time { return a.ticker.C }

func (a tickerAdapter) Stop() { a.ticker.Stop() }

// glogLogger adapts glog to the pkg.Logger interface.
type glogLogger struct{}

func (glogLogger) Errorf(format string, args ...any) {
	glog.Errorf(format, args...)
}

// createHTTPServer serves the mandatory triple (/healthz, /readiness,
// /metrics) plus the canonical admin surface (/setloglevel, /gc).
func (a *application) createHTTPServer() run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))
		router.Path("/gc").Handler(libhttp.NewGarbageCollectorHandler())
		glog.V(2).Infof("http server listening on %s", a.Listen)
		return libhttp.NewServer(a.Listen, router).Run(ctx)
	}
}
