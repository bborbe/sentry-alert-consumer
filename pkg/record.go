// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"encoding/json"

	"github.com/bborbe/errors"
)

// Record is one message on the {branch}-raw-sentry-alert-input topic as
// published by sentry-proxy's Kafka tee (spec 001). The record JSON key set is
// exactly {body, project, received_at, outcome}:
//
//   - body: the raw Sentry envelope bytes, unparsed by the producer
//   - project: the second path segment of the Sentry ingest path
//     (/api/{project_id}/envelope/), literal "unknown" when it does not match
//   - received_at: RFC3339 receive timestamp from the proxy's injected clock
//   - outcome: "forwarded" | "rejected" | "upstream_error"
//
// See the consumer contract in the task "Analyse Sentry Alerts Directly From
// the Kafka Topic" § Consumer Contract.
type Record struct {
	Body       string `json:"body"`
	Project    string `json:"project"`
	ReceivedAt string `json:"received_at"`
	Outcome    string `json:"outcome"`
}

// ParseRecord validates and decodes one topic message into a Record. A missing
// body or received_at is a hard error: neither can be recovered downstream.
func ParseRecord(ctx context.Context, data []byte) (Record, error) {
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, errors.Wrapf(ctx, err, "parse record")
	}
	if r.Body == "" {
		return Record{}, errors.Errorf(ctx, "record has empty body")
	}
	if r.ReceivedAt == "" {
		return Record{}, errors.Errorf(ctx, "record has empty received_at")
	}
	return r, nil
}
