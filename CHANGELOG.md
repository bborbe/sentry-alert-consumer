# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

## Unreleased

- chore: update Go to 1.27.3 and github.com/IBM/sarama to v1.61.1, github.com/bborbe/agent to v0.114.0, github.com/bborbe/cqrs to v0.6.12, github.com/bborbe/errors to v1.6.3, github.com/bborbe/http to v1.26.30, github.com/bborbe/kafka to v1.28.5, github.com/bborbe/log to v1.7.5, github.com/bborbe/run to v1.11.2, github.com/bborbe/sentry to v1.10.5, github.com/bborbe/service to v1.11.1, github.com/bborbe/time to v1.27.18, github.com/onsi/ginkgo/v2 to v2.33.1, github.com/onsi/gomega to v1.44.0, github.com/prometheus/client_golang to v1.25.0

## v0.2.5

- fix: re-key an enriched record on the numeric Sentry issue id. `enrichIssue`
  wrote back `NumericID` and `IssueURL` but never `Key`, so the published
  `CreateTaskCommand` stayed keyed on the envelope-derived identity — a
  per-event `event-<event_id>` for a forwarded event carrying no
  exception/message, which nothing can dedup. Two events of one issue on the
  same day therefore produced two analyzer tasks even though the lookup had
  resolved both to the same issue (measured 2026-10-19 on prod: 3 of 16
  forwarded records). The re-key covers every successful enrichment, so the
  32-hex derived hashes collapse too.

## v0.2.4

- fix: publish an analyzer task only for `forwarded` records. A `rejected` or
  `upstream_error` event was never stored by Sentry, so it has no issue identity
  and `deriveIssueKey` falls back to the per-event `event-<event_id>` key, which
  nothing can dedup — one task per rejected event, measured 2026-10-18 on dev at
  73 of 74 processed records, against a ~12/day baseline. Non-forwarded records
  are now counted as `messages_total{result="skipped_non_forwarded"}` and skipped
  before parsing; `events_total{outcome}` still counts every outcome, so the
  rejection rate stays alertable on its own.

## v0.2.3

- fix: report `eventAge` on the `enriched` line as the offset to the **first lookup attempt** rather than the offset to the log line. The field is read as evidence that a lookup happened inside Sentry's ingest-indexing window, but `time.Since(eventTime)` was evaluated *after* `IssueRef` returned, so it folded the dedicated 404 schedule's +1/+3/+6/+10 s waits into the number: a fresh event that retried before succeeding read as an old one, which is precisely the distinction the field exists to make. Observed on dev 2026-10-14 — an event logged `eventAge=7.615s` whose actual first lookup ran at +0.632 s, leaving the parent task's `eventAge < 3 s` criterion readable only by deriving the offset from raw log timestamps. The clock is now injected (`libtime.CurrentDateTimeGetter`) rather than read from `time.Now()`, so the specs assert the logged value exactly, and they are pinned to fail against the previous expression.

## v0.2.2

- fix: count a present-but-unparseable envelope event payload as `payload_parse_error` and log it with the event_id, instead of dropping it silently — `parseEventPayload` returned nil on a JSON unmarshal failure with no log and no metric, so the record degraded to the header/body-hash identity and was still counted as `processed`; that silence is what let the bare-array exception (v0.1.6) and bare-string message (PR #11) defects run undetected for weeks. The label is pre-initialised so `rate()` has a series from startup, and it stays distinct from `parse_error`, which is a Kafka *record* that did not parse. A payload that is absent entirely is not counted — only one that was present and unreadable.

## v0.2.1

- fix: retry a 404 from the eventids lookup on a dedicated schedule (+1/+3/+6/+10 s, 4 extra lookups) when the event is still inside Sentry's ingest-indexing window, so an event Sentry has stored but not yet indexed is enriched instead of silently keeping its derived key. A 404 was treated as permanent, so every lookup landing within a few seconds of the event's creation produced a derived-key task and an `unanalyzable` verdict — reproduced at +2.58 s and +0.91 s after `date_created`, and measured on the prod consumer as 0 successes in 61 attempts. The schedule is deliberately separate from the error-retry budget (which stays at 3 attempts / 2 s and still does not retry a 404): the error budget answers "is Sentry having a moment?", the schedule answers "has Sentry finished indexing this event yet?". It is skipped for events older than 30 s, because a record replayed from the topic has a real miss and holding a partition 10 s per record would stall a 45-miss replay burst for minutes. The miss is now counted as `enrich_miss` (pre-initialised, so `rate()` has a series from startup) rather than logged at V(2) only — the counter is what makes a return of this failure visible instead of silent.
- fix: bump `osv-scanner` to v2.6.0 and `golang.org/x/net` to v0.60.0 so the Linux vulnerability gates stop failing. v2.3.1 pins `golang.org/x/tools` v0.38.0, whose SSA builder aborts with `unexpected expr: *ast.KeyValueExpr` on the promoted-field composite-literal key Go 1.27 permits in the Linux stdlib, so a repo on the old pin passes locally on darwin and fails only in Linux CI. `x/net` v0.58.0 carries `GO-2026-6603/6610/6611/6612/6617`, which fail both `vulncheck` and `trivy`.

## v0.2.0

- fix: accept the bare-string `message` form (`"message":"<text>"`) the Sentry protocol allows — `message` declared only the `{"formatted":...}` object, so such payloads failed to unmarshal, `Envelope.Event` stayed nil, and the task fell to a "(no message)" `event-<id>` key; the sentry-heartbeat emitter sends exactly this form
- feat: skip the sentry-heartbeat liveness event (`logger: sentry-heartbeat`) before any task or enrichment lookup, counted as `skipped_heartbeat` — every 2h heartbeat previously became an unanalyzable analyzer task (~12/day)
- chore: bump Go 1.27.1 → 1.27.2 and golang.org/x/net v0.59.0 → v0.60.0 to clear GO-2026-6599..6611 stdlib/net/http advisories failing `vulncheck`
- chore: bump golangci-lint v2.13.1 → v2.14.0 — v2.13.1 cannot read Go 1.27.2 stdlib export data (version 5 > max 4), failing `lint` typecheck

## v0.1.6

- fix: accept the bare-array `exception` form sentry-go puts on the topic — `eventPayload.Exception` declared only the Sentry protocol's `{"values":[...]}` object, so every Go event's payload failed to unmarshal, `Envelope.Event` stayed nil, and `deriveIssueKey` fell through to a per-event `event-<id>` key for *every* event; the parse failure was swallowed silently (`parseEventPayload` returns nil with no log and no metric), which is why the mismatch ran undetected until the 2026-09-21/22 flood produced 1,189 then 1,945 Analyze-Sentry task files and fired `AgentQueueHigh` on nukeprod. Both shapes are now accepted, so events sharing one exception hash to one issue key and controller dedup collapses the burst to a single task.

## v0.1.5

- feat: retry the Sentry `eventids` lookup with bounded backoff (≤3 attempts, ≤2s total) on 429/502/503/504/transport failures, honouring `Retry-After` in both delay-seconds and HTTP-date form — burst rate limiting (three 429s ~230ms apart, observed on dev) previously abandoned enrichment on the first 429; 404 and a bare 500 stay terminal so a permanent miss never burns quota
- feat: split the `enrich_error` metric label — `enrich_retry_exhausted` now marks a lookup the client retried and gave up on, distinguishing sustained rate limiting from a one-off failure

## v0.1.4

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
