// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tasktemplate_test

import (
	"bytes"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg/tasktemplate"
)

// data mirrors the taskbuilder render data for a representative issue.
type data struct {
	ShortID  string
	Date     string
	Title    string
	Project  string
	IssueURL string
}

var sample = data{
	ShortID:  "SENTRY-TEST-1",
	Date:     "2026-08-23",
	Title:    "boom",
	Project:  "nuke-prod",
	IssueURL: "https://bborbe.sentry.io/issues/1/",
}

func render(tmpl *template.Template) string {
	var buf bytes.Buffer
	Expect(tmpl.Execute(&buf, sample)).To(Succeed())
	return buf.String()
}

var _ = Describe("tasktemplate defaults", func() {
	It("DefaultTitle renders a single-line title (no trailing newline)", func() {
		out := render(tasktemplate.DefaultTitle)
		Expect(out).To(Equal("Analyze Sentry issue SENTRY-TEST-1 - 2026-08-23"))
		Expect(out).NotTo(ContainSubstring("\n"))
	})

	It("DefaultBody renders the deployment-agnostic default", func() {
		out := render(tasktemplate.DefaultBody)
		Expect(out).To(Equal(
			"# Analyze Sentry issue SENTRY-TEST-1 - 2026-08-23\n\n" +
				"Active unresolved Sentry alert (last activity 2026-08-23) in project nuke-prod.\n" +
				"- Issue: SENTRY-TEST-1\n" +
				"- Project: nuke-prod\n" +
				"- Title: boom\n" +
				"- URL: https://bborbe.sentry.io/issues/1/\n",
		))
	})

	It("DefaultBody stays vault-agnostic — no wikilinks", func() {
		Expect(render(tasktemplate.DefaultBody)).NotTo(ContainSubstring("[["))
	})
})
