# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

## Unreleased

- feat: enrich forwarded events with Sentry's numeric issue id (`GET /api/0/organizations/{org}/eventids/{event_id}/`) so the emitted task carries the numeric `short_id` + `issue_url` the analyzer's live-state fetch (`sentry-read.sh`) requires; rejected events skip the lookup (guaranteed 404 on a quota-limited org), and a miss or a failed lookup still publishes the derived-key task
- chore: add `goUpdate.autoUpdate` to `.maintainer.yaml` so github-update-go-agent keeps the repo in the fleet sweep — the key was absent and read as `false`, silently excluding it

## v0.1.3

- feat: carry the envelope's exception snapshot (type, value, module, top stack frames) into the derived-key task body — the analyzer's no-ID path needs more than the title to differentiate real bugs from noise

## v0.1.2

- feat: export the consumer's own Kafka lag (`sentry_alert_consumer_kafka_lag`) — the broken-consumer signal the lag alert thresholds on, sourced from this consumer's position instead of the strimzi-topic-reader's per-read cache

## v0.1.1

- fix: per-event issue key uses hyphen, not title-forbidden colon (live e2e catch, 2026-09-10)

## v0.1.0

- feat: initial sentry-alert-consumer scaffold (Kafka topic → CreateTaskCommand)
- feat: consume {branch}-raw-sentry-alert-input, derive envelope-based issue identity, emit CreateTaskCommand per (issue-key, date)
