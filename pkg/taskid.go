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

// DeriveTaskIDKafka returns a UUID5 derived deterministically from the issue
// key alone, for Kafka-native consumers.
//
// Uniqueness set rationale (per the consumer contract):
//   - Same issue key → same task_id on every day the alert stream re-delivers
//     it, so the controller's identifier index resolves a later emit to the
//     existing file. A task still open (human_review or earlier) is refused as
//     ErrTaskAlreadyExists instead of being duplicated.
//   - The controller materializes a fresh non-terminal task in place when the
//     file it resolves to is terminal ("completed"/"aborted"), so an issue
//     that recurs after its analysis finished is analysed again without a
//     second file.
//   - The "kafka:" prefix keeps this identity space disjoint from the
//     collector's "sentry:" space (cmd/create-tasks), so a derived task never
//     collides with a collector-emitted task for the same issue.
//
// The date is deliberately NOT part of the key: keying on (issue-key, date)
// produced one task per issue per active day, which is the duplicate flood this
// consumer used to emit.
func DeriveTaskIDKafka(issueKey string) uuid.UUID {
	return uuid.NewSHA1(taskIDNamespace, []byte("kafka:"+issueKey))
}
