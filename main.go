// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command sentry-alert-consumer consumes the {branch}-raw-sentry-alert-input
// Kafka topic and publishes one CreateTaskCommand per derived (issue-key,
// date) so the sentry-analyzer-agent can classify each alert.
//
// Bootstrap scaffold: the consumer implementation lands via the
// feature/consumer PR (see the task "Analyse Sentry Alerts Directly From the
// Kafka Topic").
package main

import (
	"context"
	"os"

	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, nil, nil))
}

type application struct{}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	return nil
}
