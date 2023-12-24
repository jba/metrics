// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jba/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

type FahrenheitDegrees float64

func (FahrenheitDegrees) Unit() string {
	return "[degF]"
}

func TestProducer(t *testing.T) {
	ctx := context.Background()
	metrics.Reset()

	wantms := []metricdata.Metrics{
		metricdata.Metrics{
			Name:        "runtime/gc/stack/starting-size",
			Description: "The stack size of new goroutines.",
			Unit:        "bytes",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: *attribute.EmptySet(),
						Value:      2048,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: false,
			},
		},
	}

	scope := instrumentation.Scope{Name: "my/scope"}
	p := NewProducer(scope, func(name string) bool {
		if name == "runtime/gc/stack/starting-size" {
			return true
		}
		if strings.HasPrefix(name, "runtime/") {
			return false
		}
		return true
	})
	r := sdkmetric.NewManualReader(sdkmetric.WithProducer(p))
	_ = sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	// We don't need the MeterProvider if we're not using OTel instruments.

	c := metrics.NewCounter[int64]("counts", "number of counts")
	c.Add(1)
	c.Add(5)
	wantms = append(wantms, metricdata.Metrics{
		Name:        "counts",
		Description: "number of counts",
		Unit:        "",
		Data: metricdata.Sum[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      6,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	})

	g := metrics.NewGauge[FahrenheitDegrees]("temperature", metrics.Summable, "temp")
	g.Set(23)
	g.Set(72)

	wantms = append(wantms, metricdata.Metrics{
		Name:        "temperature",
		Description: "temp",
		Unit:        "[degF]",
		Data: metricdata.Sum[float64]{
			DataPoints: []metricdata.DataPoint[float64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      72.0,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: false,
		},
	})

	g2 := metrics.NewGauge[float64]("fractionUsed", metrics.NonSummable, "fraction of something in use")
	g2.Set(0.6)

	wantms = append(wantms, metricdata.Metrics{
		Name:        "fractionUsed",
		Description: "fraction of something in use",
		Data: metricdata.Gauge[float64]{
			DataPoints: []metricdata.DataPoint[float64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      0.6,
				},
			},
		},
	})

	metrics.RegisterObservableGauge("remainingFuel", metrics.Summable, func() Gallons { return 1.5 }, "remaining fuel")

	wantms = append(wantms, metricdata.Metrics{
		Name:        "remainingFuel",
		Description: "remaining fuel",
		Unit:        "[gal_us]",
		Data: metricdata.Sum[float64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.DataPoint[float64]{
				{
					Attributes: *attribute.EmptySet(),
					Value:      1.5,
				},
			},
		},
	})

	h := metrics.NewHistogram[time.Duration]("requestTimes", []time.Duration{time.Millisecond, 100 * time.Millisecond, time.Second},
		"request durations")
	h.Record(50 * time.Millisecond)
	h.Record(10 * time.Millisecond)
	h.Record(80 * time.Millisecond)
	h.Record(3 * time.Second)
	h.Record(200 * time.Millisecond)
	h.Record(700 * time.Millisecond)

	fm := float64(time.Millisecond)
	wantms = append(wantms, metricdata.Metrics{
		Name:        "requestTimes",
		Description: "request durations",
		Unit:        "ns",
		Data: metricdata.Histogram[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[int64]{
				{
					Attributes:   *attribute.EmptySet(),
					BucketCounts: []uint64{0, 3, 2, 1},
					Bounds:       []float64{fm, 100 * fm, 1000 * fm},
					Count:        6,
				},
			},
		},
	})

	////////////////
	// Groups

	cg := metrics.NewCounterGroup[int64, RequestAttrs]("requests", "requests by method and status")
	cg.At(RequestAttrs{Method: "GET", Status: 200}).Add(3)
	cg.At(RequestAttrs{Method: "POST", Status: 400}).Add(7)
	cg.At(RequestAttrs{Method: "GET", Status: 200}).Add(1)

	wantms = append(wantms, metricdata.Metrics{
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
	})

	type SX struct {
		X int
	}
	gg := metrics.NewGaugeGroup[float64, SX]("gg", metrics.NonSummable, "ggd")
	gg.At(SX{2}).Set(2)
	wantms = append(wantms, metricdata.Metrics{
		Name:        "gg",
		Description: "ggd",
		Data: metricdata.Gauge[float64]{
			DataPoints: []metricdata.DataPoint[float64]{
				{
					Attributes: attribute.NewSet(attribute.Int("x", 2)),
					Value:      2,
				},
			},
		},
	})

	hg := metrics.NewHistogramGroup[int64, SX]("hg", []int64{1, 10}, "hgd")
	hg.At(SX{3}).Record(5)
	hg.At(SX{3}).Record(-1)
	wantms = append(wantms, metricdata.Metrics{
		Name:        "hg",
		Description: "hgd",
		Unit:        "",
		Data: metricdata.Histogram[int64]{
			Temporality: metricdata.CumulativeTemporality,
			DataPoints: []metricdata.HistogramDataPoint[int64]{
				{
					Attributes:   attribute.NewSet(attribute.Int("x", 3)),
					BucketCounts: []uint64{1, 1, 0},
					Bounds:       []float64{1, 10},
					Count:        2,
				},
			},
		},
	})

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
					Name: "my/scope",
				},
				Metrics: wantms,
			},
		},
	}

	diff := cmp.Diff(want, got,
		cmpopts.IgnoreFields(metricdata.DataPoint[int64]{}, "Time", "StartTime"),
		cmpopts.IgnoreFields(metricdata.DataPoint[float64]{}, "Time", "StartTime"),
		cmpopts.IgnoreFields(metricdata.HistogramDataPoint[int64]{}, "Time", "StartTime"),
		cmpopts.IgnoreFields(metricdata.HistogramDataPoint[float64]{}, "Time", "StartTime"),
		cmpopts.EquateComparable(attribute.Set{}),
		cmp.Comparer(extremaEqual[int64]),
		cmp.Comparer(extremaEqual[float64]),
		cmpopts.SortSlices(dataPointLess),
	)
	if diff != "" {
		t.Errorf("(-want, +got):\n%s", diff)
	}

}

func extremaEqual[N int64 | float64](e1, e2 metricdata.Extrema[N]) bool {
	v1, d1 := e1.Value()
	v2, d2 := e2.Value()
	return v1 == v2 && d1 == d2
}

func dataPointLess(d1, d2 metricdata.DataPoint[int64]) bool {
	return d1.Value < d2.Value
}

// var enc = attribute.DefaultEncoder()

// func attributeSetLess(s1, s2 attribute.Set) bool {
// 	return s1.Encoded(enc) < s2.Encoded(enc)
// }

type RequestAttrs struct {
	Method string
	Status int
}
type Gallons float64

func (Gallons) Unit() string { return "[gal_us]" }
