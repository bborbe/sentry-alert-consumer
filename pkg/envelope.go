// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bborbe/errors"
)

// envelopeHeader is the first line of a Sentry envelope. The raw envelope bytes
// are the tee's {body} field; parsing is deliberately consumer-side (the tee
// never parses the envelope — see spec 001 Non-goals).
type envelopeHeader struct {
	EventID string `json:"event_id"`
	SentAt  string `json:"sent_at"`
}

// eventPayload is the JSON payload of the envelope's "event" item, carrying the
// fields the consumer needs to derive an issue identity.
type eventPayload struct {
	EventID   string    `json:"event_id"`
	Timestamp string    `json:"timestamp"`
	Level     string    `json:"level"`
	Message   *message  `json:"message"`
	LogEntry  *message  `json:"logentry"`
	Exception exception `json:"exception"`
}

type message struct {
	Formatted string `json:"formatted"`
	Message   string `json:"message"`
}

type exception struct {
	Values []exceptionValue `json:"values"`
}

type exceptionValue struct {
	Type       string         `json:"type"`
	Value      string         `json:"value"`
	Module     string         `json:"module"`
	Stacktrace exceptionTrace `json:"stacktrace"`
}

// exceptionTrace is the exception's stacktrace, with frames in stack order
// (innermost first, as Sentry SDKs send them).
type exceptionTrace struct {
	Frames []exceptionFrame `json:"frames"`
}

// exceptionFrame is one stack frame. InApp is nil when Sentry did not classify
// the frame (absent or null in the payload), so the task body can preserve the
// unknown case rather than collapsing it to false.
type exceptionFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	LineNo   int    `json:"lineno"`
	InApp    *bool  `json:"in_app"`
}

// Envelope holds the identity fields extracted from one raw Sentry envelope.
// Event may be nil when the event payload is missing or unparseable — the
// caller then falls back to the envelope header's event_id (or the body hash),
// never dropping the record.
type Envelope struct {
	EventID string
	SentAt  string
	Event   *eventPayload
}

// ParseEnvelope extracts the envelope header (event_id) and, when present and
// parseable, the first "event" item's payload. It is best-effort by design:
// only an empty envelope or an unparseable header line is a hard error — a
// malformed event payload degrades to the header identity instead.
func ParseEnvelope(ctx context.Context, raw string) (Envelope, error) {
	lines := splitEnvelopeLines(raw)
	if len(lines) == 0 {
		return Envelope{}, errors.Errorf(ctx, "empty envelope")
	}
	var hdr envelopeHeader
	if err := json.Unmarshal([]byte(lines[0]), &hdr); err != nil {
		return Envelope{}, errors.Wrapf(ctx, err, "parse envelope header")
	}
	e := Envelope{EventID: hdr.EventID, SentAt: hdr.SentAt}
	if ev := parseEventItem(lines[1:]); ev != nil {
		e.Event = ev
		if e.EventID == "" && ev.EventID != "" {
			e.EventID = ev.EventID
		}
	}
	return e, nil
}

// splitEnvelopeLines splits the raw envelope on newlines and trims leading
// blank lines, returning an empty slice for a fully blank input.
func splitEnvelopeLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}

// parseEventItem scans the item lines after the envelope header for the first
// "event" item and returns its payload, or nil when absent or unparseable.
func parseEventItem(lines []string) *eventPayload {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var itemHdr struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(trimmed), &itemHdr); err != nil || itemHdr.Type == "" {
			continue // not an item header line — skip
		}
		if itemHdr.Type != "event" {
			continue
		}
		return parseEventPayload(lines[i+1:])
	}
	return nil
}

// parseEventPayload parses the first non-empty line after the "event" item
// header as the event payload (SDKs send compact single-line event JSON). A
// parse failure returns nil — the header's event_id still identifies the event.
func parseEventPayload(lines []string) *eventPayload {
	for _, line := range lines {
		payload := strings.TrimSpace(line)
		if payload == "" {
			continue
		}
		var ev eventPayload
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil
		}
		return &ev
	}
	return nil
}
