// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package tasktemplate holds the built-in default title/body templates for the
// emitted sentry-issue-analyzer task, authored as embedded markdown files. A
// deployment overrides them via the TASK_TITLE_TEMPLATE / TASK_BODY_TEMPLATE
// envs; these defaults are deployment-agnostic so the watcher is reusable as-is.
//
// The templates are Go text/templates rendered against the task-template data
// (fields: ShortID, Date, Title, Project, IssueURL).
package tasktemplate

import (
	_ "embed"
	"strings"
	"text/template"
)

//go:embed default-title.md
var defaultTitle string

//go:embed default-body.md
var defaultBody string

// DefaultTitle is the built-in title template used when no TASK_TITLE_TEMPLATE
// override is configured. Trailing newlines from the embedded file are trimmed
// so the rendered title stays a single line.
var DefaultTitle = template.Must(
	template.New("title").Parse(strings.TrimRight(defaultTitle, "\n")),
)

// DefaultBody is the built-in body template used when no TASK_BODY_TEMPLATE
// override is configured.
var DefaultBody = template.Must(template.New("body").Parse(defaultBody))
