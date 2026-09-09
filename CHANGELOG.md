# Changelog

## Unreleased

- feat: initial sentry-alert-consumer scaffold (Kafka topic → CreateTaskCommand)
- feat: consume {branch}-raw-sentry-alert-input, derive envelope-based issue identity, emit CreateTaskCommand per (issue-key, date)
