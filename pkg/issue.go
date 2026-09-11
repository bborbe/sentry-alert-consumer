// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/bborbe/errors"
)

// Issue is the per-record derived grouping identity. Sentry's own issue
// grouping is server-side and absent from the raw envelope, so the consumer
// derives its own key (see the consumer contract § Grouping).
type Issue struct {
	Key        string // stable grouping identity (issue key)
	EventID    string
	Title      string
	Project    string
	Outcome    string
	ReceivedAt string
	Date       string // UTC calendar date ("2006-01-02") of the event
	// Exception snapshot carried into the task body so the analyzer can
	// classify derived-key (no-ID) tasks from the envelope's own payload —
	// the title alone is not enough to differentiate real bugs from noise.
	ExceptionType   string
	ExceptionValue  string
	ExceptionModule string
	Trace           []Frame
}

// Frame is one stack frame from the envelope's exception, in stack order
// (innermost first). InApp preserves Sentry's unknown classification.
type Frame struct {
	Filename string
	Function string
	LineNo   int
	InApp    *bool
}

// BuildIssue derives the grouping identity for one topic record per the
// consumer contract:
//
//   - dedup key: the envelope's event_id
//   - issue key: first exception type+value+module → message hash → event_id
//     (per-event fallback) → body hash (unparseable-envelope fallback)
//   - title: exception value → message → "(no message)"
//   - date: the event timestamp's UTC date, falling back to received_at
func BuildIssue(ctx context.Context, env Envelope, rec Record) (Issue, error) {
	key := deriveIssueKey(env, rec)
	title := deriveTitle(env)
	exceptionType, exceptionValue, exceptionModule, frames := exceptionFromEvent(env.Event)

	timestamp := rec.ReceivedAt
	if env.Event != nil && env.Event.Timestamp != "" {
		timestamp = env.Event.Timestamp
	}
	date, err := deriveDate(ctx, timestamp)
	if err != nil {
		return Issue{}, errors.Wrapf(ctx, err, "derive date for record")
	}

	return Issue{
		Key:             key,
		EventID:         env.EventID,
		Title:           title,
		Project:         rec.Project,
		Outcome:         rec.Outcome,
		ReceivedAt:      rec.ReceivedAt,
		Date:            date,
		ExceptionType:   exceptionType,
		ExceptionValue:  exceptionValue,
		ExceptionModule: exceptionModule,
		Trace:           frames,
	}, nil
}

// exceptionFromEvent extracts the first exception's type, value, module and
// stack frames from the event payload, for the derived-key task body. All four
// return zero values when the payload holds no exception.
func exceptionFromEvent(ev *eventPayload) (string, string, string, []Frame) {
	if ev == nil {
		return "", "", "", nil
	}
	if values := ev.Exception.Values; len(values) > 0 {
		v := values[0]
		var frames []Frame
		for _, f := range v.Stacktrace.Frames {
			frames = append(frames, Frame(f))
		}
		return v.Type, v.Value, v.Module, frames
	}
	return "", "", "", nil
}

// deriveIssueKey produces the envelope-derived issue identity. The first
// exception's type+value+module is the strongest signal (Sentry's own
// fingerprint is built from the same material); the message is the next-best;
// a bare event_id yields a per-event unit; a wholly unparseable envelope falls
// back to a body hash so no record is silently dropped.
func deriveIssueKey(env Envelope, rec Record) string {
	if key := keyFromEvent(env.Event); key != "" {
		return key
	}
	if env.EventID != "" {
		// Per-event identity, no hash needed. The hyphen (not a colon)
		// separator keeps the key out of the CreateTaskCommand title's
		// forbidden-character set (observed 2026-09-10: "event:<id>" made
		// every per-event task fail title validation).
		return "event-" + env.EventID
	}
	return hashKey("body:" + rec.Body)
}

// keyFromEvent derives the group key from the event payload, or "" when the
// payload holds no identity material (nil or empty exception/message).
func keyFromEvent(ev *eventPayload) string {
	if ev == nil {
		return ""
	}
	if values := ev.Exception.Values; len(values) > 0 {
		v := values[0]
		if v.Type != "" || v.Value != "" {
			return hashKey(strings.Join([]string{v.Module, v.Type, v.Value}, ":"))
		}
	}
	if formatted := messageText(ev.Message); formatted != "" {
		return hashKey("msg:" + formatted)
	}
	if formatted := messageText(ev.LogEntry); formatted != "" {
		return hashKey("msg:" + formatted)
	}
	return ""
}

// deriveTitle picks the most useful one-line title from the envelope: the
// exception value (what failed), else the message, else a placeholder.
func deriveTitle(env Envelope) string {
	if title := titleFromEvent(env.Event); title != "" {
		return title
	}
	return "(no message)"
}

// titleFromEvent picks the title from the event payload, or "" when the
// payload holds no title material (nil or empty exception/message).
func titleFromEvent(ev *eventPayload) string {
	if ev == nil {
		return ""
	}
	if values := ev.Exception.Values; len(values) > 0 {
		v := values[0]
		if v.Value != "" {
			return v.Value
		}
		if v.Type != "" {
			return v.Type
		}
	}
	if formatted := messageText(ev.Message); formatted != "" {
		return formatted
	}
	if formatted := messageText(ev.LogEntry); formatted != "" {
		return formatted
	}
	return ""
}

// messageText returns the formatted message text, preferring the SDK's
// pre-formatted field over the raw template.
func messageText(m *message) string {
	if m == nil {
		return ""
	}
	if m.Formatted != "" {
		return m.Formatted
	}
	return m.Message
}

// hashKey returns the hex SHA-256 of s, truncated to 32 chars — a stable,
// collision-resistant group key that stays URL/path-safe.
func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:32]
}

// deriveDate returns the UTC calendar date of an RFC3339 timestamp.
func deriveDate(ctx context.Context, timestamp string) (string, error) {
	if timestamp == "" {
		return "", errors.Errorf(ctx, "no timestamp")
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "parse timestamp %q", timestamp)
	}
	return t.UTC().Format("2006-01-02"), nil
}

// deriveStage returns the frontmatter stage for one record, derived from its
// project slug (nuke-prod → prod, nuke-dev → dev); anything else falls back to
// the global stage so a non-nuke project still gets a valid stage. Mirrors
// cmd/create-tasks' deriveStage, which keys on short_id prefix and project
// slug; the consumer has no short_id, so the project slug is the signal.
func DeriveStage(project, fallback string) string {
	switch {
	case strings.HasPrefix(strings.ToLower(project), "nuke-prod"):
		return "prod"
	case strings.HasPrefix(strings.ToLower(project), "nuke-dev"):
		return "dev"
	}
	return fallback
}
