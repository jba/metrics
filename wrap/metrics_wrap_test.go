// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kylelemons/godebug/pretty"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	ometric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

type liter float64

func (liter) Unit() string { return "L" }

func TestFull(t *testing.T) {
	ctx := context.Background()
	r := ometric.NewManualReader()

	mp := ometric.NewMeterProvider(ometric.WithReader(r))
	meter := mp.Meter("testing")

	ic := NewCounter[int64](meter, "reqs", "number of requests")
	ic.Add(1)
	ic.Add(7)
	wm1 := metricdata.Metrics{
		Name:        "reqs",
		Description: "number of requests",
		Unit:        "",
		Data: metricdata.Sum[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      8,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	}

	fc := NewCounter[liter](meter, "water", "total amount of water")
	fc.Add(3.5)
	fc.Add(liter(math.Inf(1)))
	fc.Add(liter(math.NaN()))
	fc.Add(4)

	wm2 := metricdata.Metrics{
		Name:        "water",
		Description: "total amount of water",
		Unit:        "L",
		Data: metricdata.Sum[float64]{
			DataPoints: []metricdata.DataPoint[float64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      7.5,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	}

	g := NewCounterGroup[int64, RequestAttrs](meter, "requests", "requests by method and status")
	g.At(RequestAttrs{Method: "GET", Status: 200}).Add(3)
	g.At(RequestAttrs{Method: "POST", Status: 400}).Add(7)
	g.At(RequestAttrs{Method: "GET", Status: 200}).Add(1)

	wm3 := metricdata.Metrics{
		Name:        "requests",
		Description: "requests by method and status",
		Unit:        "",
		Data: metricdata.Sum[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(attribute.String("method", "GET"), attribute.Int("status", 200)),
					Value:      4,
				},
				{
					Attributes: attribute.NewSet(attribute.String("method", "POST"), attribute.Int("status", 400)),
					Value:      7,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	}

	////////////////

	var got metricdata.ResourceMetrics
	if err := r.Collect(ctx, &got); err != nil {
		t.Fatal(err)
	}

	want := metricdata.ResourceMetrics{
		Resource: resource.Default(),
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Scope: instrumentation.Scope{
					Name: "testing",
				},
				Metrics: []metricdata.Metrics{wm1, wm2, wm3},
			},
		},
	}

	diff := cmp.Diff(want, got,
		cmpopts.IgnoreFields(metricdata.DataPoint[int64]{}, "Time", "StartTime"),
		cmpopts.IgnoreFields(metricdata.DataPoint[float64]{}, "Time", "StartTime"),
		cmpopts.EquateComparable(attribute.Set{}))
	if diff != "" {
		t.Errorf("(-want, +got):\n%s", diff)
		cfg := pretty.Config{
			Diffable:            false,
			PrintStringers:      true,
			PrintTextMarshalers: true,
			Formatter: map[reflect.Type]any{
				reflect.TypeOf(attribute.Set{}): func(s attribute.Set) string {
					return fmt.Sprintf("%+v", s.ToSlice())
				},
			},
		}
		cfg.Print(got)
	}
	// data, err := json.MarshalIndent(rm.ScopeMetrics[0], "", "    ")
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// fmt.Printf("%s\n", data)

	//fmt.Printf("%+v\n", rm.ScopeMetrics[0])

}

type RequestAttrs struct {
	Method string
	Status int
}
