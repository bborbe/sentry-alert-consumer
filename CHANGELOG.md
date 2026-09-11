# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

## Unreleased

- feat: carry the envelope's exception snapshot (type, value, module, top stack frames) into the derived-key task body — the analyzer's no-ID path needs more than the title to differentiate real bugs from noise

## v0.1.2

- feat: export the consumer's own Kafka lag (`sentry_alert_consumer_kafka_lag`) — the broken-consumer signal the lag alert thresholds on, sourced from this consumer's position instead of the strimzi-topic-reader's per-read cache

## v0.1.1

- fix: per-event issue key uses hyphen, not title-forbidden colon (live e2e catch, 2026-09-10)

## v0.1.0

- feat: initial sentry-alert-consumer scaffold (Kafka topic → CreateTaskCommand)
- feat: consume {branch}-raw-sentry-alert-input, derive envelope-based issue identity, emit CreateTaskCommand per (issue-key, date)
