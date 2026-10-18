# sentry-alert-consumer

Consumes the `{branch}-raw-sentry-alert-input` Kafka topic and publishes one
`CreateTaskCommand` per derived (issue-key, date) so the `sentry-analyzer-agent`
can classify each alert — replacing the `sentry-collector-agent`'s token-REST
fan-out as the trigger of the Sentry pipeline.

The consumer is the **Kafka-native trigger** front of the Sentry pipeline. It
reads the complete alert stream (including alerts Sentry never accepted —
spike-protected or over-quota, which the API can never show), derives an issue
identity from each raw Sentry envelope, and emits one "Analyze Sentry issue
<KEY> - <date>" task into the Personal vault. Sentry API access continues only
as per-task enrichment inside the analyzer (`sentry-read.sh`), never as the
trigger.

## How It Works

For each message on the alert topic:

1. **Parse the tee record** — JSON `{body, project, received_at, outcome}` as
   published by `sentry-proxy`'s Kafka tee (spec 001). `body` is the raw Sentry
   envelope; `outcome` is `forwarded` / `rejected` / `upstream_error`.
2. **Gate on the outcome** — only a `forwarded` event was stored by Sentry, so
   only it has an issue to analyse. A `rejected` or `upstream_error` record is
   counted (`skipped_non_forwarded`) and skipped before any parsing: it has no
   issue identity, so its key would be the per-event `event-<event_id>`, which
   nothing can dedup — one analyzer task per rejected event. The rejection rate
   stays observable through `events_total{outcome="rejected"}`.
3. **Parse the envelope** — extract `event_id` and the event payload
   (exception, message). Best-effort: a missing or malformed event payload
   degrades to the envelope header's `event_id`, never dropping the record.
4. **Derive the issue** — grouping per the consumer contract:
   - dedup key: `event_id`
   - issue key: first exception `type`+`value`+`module` → `message` hash →
     `event_id` (per-event fallback) → body hash (unparseable-envelope fallback)
   - title: exception value → message → "(no message)"
   - date: the event timestamp's UTC calendar date, falling back to
     `received_at`
5. **Enrich forwarded events** — only for `outcome: forwarded`, resolve the
   event's numeric Sentry issue id via
   `GET {SENTRY_URL}/api/0/organizations/{SENTRY_ORG}/eventids/{event_id}/`
   (scope `org:read`) and emit it as `short_id` + `issue_url`, so the analyzer
   can run its live-state fetch (`sentry-read.sh`, which requires `^[0-9]+$`).
   A rejected event was never stored by Sentry, so its lookup is a guaranteed
   404 that only burns quota — it is skipped. A miss (forwarded but dropped by
   quota) or any lookup failure keeps the derived key and still publishes the
   task: enrichment never drops an alert.
6. **Publish one `CreateTaskCommand`** per (issue-key, date). Task identity is
   `UUID5("kafka:" + issue-key + ":" + date)` over the same namespace as the
   collector's `/create-tasks`, so the two identity spaces never collide and
   controller dedup makes re-emits (including full-topic replays after a
   restart) a no-op.

The emitted task contract is preserved from `cmd/create-tasks`:
`task_type: sentry-issue-analyzer`, `assignee: sentry-analyzer-agent`, `stage`
derived from the project slug (`nuke-prod`→prod, `nuke-dev`→dev), `status:
in_progress`, Personal-vault target.

## Configuration

| Env | Default | Purpose |
|-----|---------|---------|
| `SENTRY_API_TOKEN` | *(empty)* | Sentry API token (scope `org:read`) for event→issue enrichment; empty disables enrichment — the binary runs and tasks keep the derived key |
| `SENTRY_URL` | `https://bborbe.sentry.io` | Sentry base URL for the enrichment lookup |
| `SENTRY_ORG` | `bborbe` | Sentry organization slug for the enrichment lookup |

## Observability

- `sentry_alert_consumer_messages_total{result}` — processed / parse_error /
  envelope_error / payload_parse_error / build_error / publish_error /
  enrich_error / enrich_retry_exhausted / enrich_miss / skipped_heartbeat /
  skipped_non_forwarded
- `sentry_alert_consumer_events_total{outcome}` — forwarded / rejected /
  upstream_error. Every outcome is counted here, including the ones that no
  longer publish a task, so the rejection rate stays alertable on its own.
- `sentry_alert_consumer_published_total{status}` — create / error
- `sentry_alert_consumer_last_event_timestamp` — Unix seconds of the last
  processed event; a silence alert thresholds on this (empty-topic vs
  broken-consumer detection is a separate alerting concern)

## Task Contract

See the task "Analyse Sentry Alerts Directly From the Kafka Topic" § Consumer
Contract for the full contract, including the silence-window and
enrichment-only Sentry API decisions.
