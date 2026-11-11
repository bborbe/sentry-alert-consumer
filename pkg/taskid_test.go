// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/bborbe/sentry-alert-consumer/pkg"
)

var _ = ginkgo.Describe("DeriveTaskIDKafka", func() {
	ginkgo.It("is deterministic for the same key", func() {
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1").String()).
			To(gomega.Equal(pkg.DeriveTaskIDKafka("key-1").String()))
	})

	ginkgo.It("differs across keys", func() {
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1")).
			To(gomega.Not(gomega.Equal(pkg.DeriveTaskIDKafka("key-2"))))
	})

	ginkgo.It("pins the namespace contract", func() {
		ns := uuid.MustParse("1b06ab0c-eede-481f-b6ee-e91b0708fba9")
		expected := uuid.NewSHA1(ns, []byte("kafka:key-1"))
		gomega.Expect(pkg.DeriveTaskIDKafka("key-1")).To(gomega.Equal(expected))
	})
})
