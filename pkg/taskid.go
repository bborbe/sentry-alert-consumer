// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"github.com/google/uuid"
)

// taskIDNamespace is the UUID5 namespace for sentry-issue-analyzer tasks.
// Stable across releases — changing it would break controller dedup.
var taskIDNamespace = uuid.MustParse("1b06ab0c-eede-481f-b6ee-e91b0708fba9")

// DeriveTaskIDKafka returns a UUID5 derived deterministically from the
// (issue-key, date) pair for Kafka-native consumers.
//
// Uniqueness set rationale (per the consumer contract):
//   - Same (issue-key, date) → same task_id → controller dedup makes re-emit a
//     no-op (including full-topic replays after a restart).
//   - A new date for the same issue → new name → new task_id → fresh daily task.
//   - The "kafka:" prefix keeps this identity space disjoint from the
//     collector's "sentry:" space (cmd/create-tasks), so a derived task never
//     collides with a collector-emitted task for the same date.
func DeriveTaskIDKafka(issueKey, date string) uuid.UUID {
	return uuid.NewSHA1(taskIDNamespace, []byte("kafka:"+issueKey+":"+date))
}
