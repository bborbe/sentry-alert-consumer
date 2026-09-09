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

// DeriveTaskID returns a UUID5 derived deterministically from the (short-id,
// date) pair (e.g. ("SENTRY-TEST-1", "2026-08-23")).
//
// Uniqueness set rationale (per [[Watcher Writing Guide]] § Deterministic task_identifier):
//   - Same (short-id, date) → same task_id → controller dedup makes re-emit a no-op.
//   - A new date for the same issue → new name → new task_id → fresh daily task.
func DeriveTaskID(shortID, date string) uuid.UUID {
	return uuid.NewSHA1(taskIDNamespace, []byte("sentry:"+shortID+":"+date))
}
