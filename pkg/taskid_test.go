// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("DeriveTaskIDKafka", func() {
	ginkgo.It("is deterministic for the same (key, date)", func() {
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1", "2026-09-09").String()).
			To(gomega.Equal(pkg.DeriveTaskIDKafka("key-1", "2026-09-09").String()))
	})

	ginkgo.It("differs across keys and dates", func() {
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1", "2026-09-09")).
			To(gomega.Not(gomega.Equal(pkg.DeriveTaskIDKafka("key-2", "2026-09-09"))))
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1", "2026-09-09")).
			To(gomega.Not(gomega.Equal(pkg.DeriveTaskIDKafka("key-1", "2026-09-10"))))
	})
})
