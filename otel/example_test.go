// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel_test

import (
	"fmt"

	"github.com/jba/metrics"
	"github.com/jba/metrics/otel"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric"
)

func Example_Producer() {
	// The package "metric" here is "go.opentelemetry.io/otel/sdk/metric".

	for _, d := range metrics.All() {
		fmt.Println(d.Name)
	}

	scope := instrumentation.Scope{}
	p := otel.NewProducer(scope, func(name string) bool {
		return name == "runtime/memory/classes/total"
	})
	r := metric.NewManualReader(metric.WithProducer(p))
	_ = r

}
