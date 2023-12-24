// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"fmt"
	"math"
	"reflect"
	rm "runtime/metrics"
	"sort"
	"strings"
	"testing"
	"time"

	ccmp "github.com/google/go-cmp/cmp"

	md "github.com/jba/metrics/metricsdata"
)

func TestMakeKeyValues(t *testing.T) {
	type A struct {
		I    int
		Bool bool
		Str  string
		f    float64
	}

	makeKVs := keyValueMaker[A]()
	got := makeKVs(A{I: 7, Bool: true, Str: "moo"})
	want := []md.KeyValue{{"bool", true}, {"i", int64(7)}, {"str", "moo"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

var testTime = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

func testSingleReader(t *testing.T, r Reader, want md.Metric) {
	t.Helper()
	gotDs := r.Descriptions()
	if g := len(gotDs); g != 1 {
		t.Fatalf("got %d descriptions, want 1", g)
	}
	gotMs := r.Read(nil)
	for _, m := range gotMs {
		setNonZeroTimes(testTime, m)
	}
	if g := len(gotMs); g != 1 {
		t.Fatalf("got %d metrics, want 1", g)
	}
	got := gotMs[0]
	// Sort for comparison.
	if got.Sum != nil {
		sortNumberDataPoints(got.Sum.DataPoints)
	} else if got.Gauge != nil {
		sortNumberDataPoints(got.Gauge.DataPoints)
	} else if got.Histogram != nil {
		sortHistogramDataPoints(got.Histogram.DataPoints)
	}
	diff := ccmp.Diff(want, got,
		ccmp.AllowUnexported(md.Number{}))
	if diff != "" {
		t.Errorf("mismatch (-want, +got):\n%s", diff)
	}
}

func lastReader() Reader {
	mu.Lock()
	defer mu.Unlock()
	return readers[len(readers)-1]
}

func sortNumberDataPoints(dps []md.NumberDataPoint) {
	sort.Slice(dps, func(i, j int) bool {
		return sortString(dps[i].Attributes) < sortString(dps[j].Attributes)
	})
}

func sortHistogramDataPoints(dps []md.HistogramDataPoint) {
	sort.Slice(dps, func(i, j int) bool {
		return sortString(dps[i].Attributes) < sortString(dps[j].Attributes)
	})
}

func sortString(kvs []md.KeyValue) string {
	var ss []string
	for _, kv := range kvs {
		ss = append(ss, fmt.Sprintf("%s=%v", kv.Key, kv.Value))
	}
	return strings.Join(ss, ",")
}

func TestNumberInstruments(t *testing.T) {
	c := NewCounter[int64]("c", "d")
	c.Add(3)
	c.Add(2)
	testSingleReader(t, lastReader(),
		md.Metric{
			Name:        "c",
			Description: "d",
			Unit:        "",
			Sum: &md.Sum{
				Temporality: md.TemporalityCumulative,
				IsMonotonic: true,
				DataPoints: []md.NumberDataPoint{
					{Time: testTime, Number: md.IntNumber(5)},
				},
			},
		})

	g := NewGauge[int64]("g", NonSummable, "d2")
	g.Set(3)
	g.Set(2)
	testSingleReader(t, lastReader(),
		md.Metric{
			Name:        "g",
			Description: "d2",
			Unit:        "",
			Gauge: &md.Gauge{
				DataPoints: []md.NumberDataPoint{
					{Time: testTime, Number: md.IntNumber(2)},
				},
			},
		})

	RegisterObservableGauge[int64]("r", Summable, func() int64 { return 11 }, "z")
	testSingleReader(t, lastReader(),
		md.Metric{
			Name:        "r",
			Description: "z",
			Unit:        "",
			Sum: &md.Sum{
				Temporality: md.TemporalityCumulative,
				IsMonotonic: false,
				DataPoints: []md.NumberDataPoint{
					{Time: testTime, Number: md.IntNumber(11)},
				},
			},
		})
}

func TestHistogram(t *testing.T) {
	h := NewHistogram("h", []int64{0, 100}, "hd")
	for _, i := range []int64{-1, 0, 0, 0, 1, 1, 1, 200, 200} {
		h.Record(i)
	}
	testSingleReader(t, lastReader(),
		md.Metric{
			Name:        "h",
			Description: "hd",
			Unit:        "",
			Histogram: &md.Histogram{
				Temporality: md.TemporalityCumulative,
				IsInt:       true,
				DataPoints: []md.HistogramDataPoint{
					{
						Time:           testTime,
						ExplicitBounds: []float64{0, 100},
						BucketCounts:   []uint64{4, 3, 2},
					},
				},
			},
		})
}

type Attrs struct {
	S string
	B bool
}

func TestGroups(t *testing.T) {
	t.Run("Counter", func(t *testing.T) {
		cg := NewCounterGroup[int64, Attrs]("cg", "d")
		cg.At(Attrs{"x", true}).Add(1)
		cg.At(Attrs{"y", false}).Add(2)
		cg.At(Attrs{"x", true}).Add(3)
		testSingleReader(t, lastReader(),
			md.Metric{
				Name:        "cg",
				Description: "d",
				Unit:        "",
				Sum: &md.Sum{
					IsMonotonic: true,
					Temporality: md.TemporalityCumulative,
					DataPoints: []md.NumberDataPoint{
						{
							Time:   testTime,
							Number: md.IntNumber(2),
							Attributes: []md.KeyValue{
								{"b", false},
								{"s", "y"},
							},
						},
						{
							Time:   testTime,
							Number: md.IntNumber(4),
							Attributes: []md.KeyValue{
								{"b", true},
								{"s", "x"},
							},
						},
					},
				},
			})
	})

	t.Run("Gauge", func(t *testing.T) {
		gg := NewGaugeGroup[int64, Attrs]("gg", NonSummable, "dgg")
		gg.At(Attrs{S: "x", B: false}).Set(4)
		gg.At(Attrs{S: "y", B: true}).Set(5)
		gg.At(Attrs{S: "x", B: false}).Set(6)
		testSingleReader(t, lastReader(),
			md.Metric{
				Name:        "gg",
				Description: "dgg",
				Unit:        "",
				Gauge: &md.Gauge{
					DataPoints: []md.NumberDataPoint{
						{
							Time:   testTime,
							Number: md.IntNumber(6),
							Attributes: []md.KeyValue{
								{"b", false},
								{"s", "x"},
							},
						},
						{
							Time:   testTime,
							Number: md.IntNumber(5),
							Attributes: []md.KeyValue{
								{"b", true},
								{"s", "y"},
							},
						},
					},
				},
			})
	})

	t.Run("ObservableGauge", func(t *testing.T) {
		ogg := NewObservableGaugeGroup[int64, Attrs]("grg", NonSummable, "dgr")
		ogg.Register(Attrs{B: false, S: "a"}, func() int64 { return 11 })
		ogg.Register(Attrs{B: true, S: "b"}, func() int64 { return 12 })
		testSingleReader(t, lastReader(),
			md.Metric{
				Name:        "grg",
				Description: "dgr",
				Unit:        "",
				Gauge: &md.Gauge{
					DataPoints: []md.NumberDataPoint{
						{
							Time:   testTime,
							Number: md.IntNumber(11),
							Attributes: []md.KeyValue{
								{"b", false},
								{"s", "a"},
							},
						},
						{
							Time:   testTime,
							Number: md.IntNumber(12),
							Attributes: []md.KeyValue{
								{"b", true},
								{"s", "b"},
							},
						},
					},
				},
			})
	})

	t.Run("Histogram", func(t *testing.T) {
		hg := NewHistogramGroup[int64, Attrs]("hg", []int64{0, 5}, "dhg")
		hg.At(Attrs{S: "x", B: false}).Record(0)
		hg.At(Attrs{S: "y", B: true}).Record(1)
		hg.At(Attrs{S: "y", B: true}).Record(7)
		hg.At(Attrs{S: "x", B: false}).Record(2)
		testSingleReader(t, lastReader(),
			md.Metric{
				Name:        "hg",
				Description: "dhg",
				Unit:        "",
				Histogram: &md.Histogram{
					Temporality: md.TemporalityCumulative,
					IsInt:       true,
					DataPoints: []md.HistogramDataPoint{
						{
							Time:           testTime,
							ExplicitBounds: []float64{0, 5},
							Attributes: []md.KeyValue{
								{"b", false},
								{"s", "x"},
							},
							BucketCounts: []uint64{1, 1, 0},
						},
						{
							Time:           testTime,
							ExplicitBounds: []float64{0, 5},
							Attributes: []md.KeyValue{
								{"b", true},
								{"s", "y"},
							},
							BucketCounts: []uint64{0, 1, 1},
						},
					},
				},
			})
	})
}

func TestConvertHistogram(t *testing.T) {
	for _, test := range []struct {
		in         rm.Float64Histogram
		wantCounts []uint64
		wantBounds []float64
	}{
		{
			in: rm.Float64Histogram{
				Counts:  []uint64{1, 2, 3},
				Buckets: []float64{math.Inf(-1), 0, 10, 20},
			},
			wantCounts: []uint64{1, 2, 3, 0},
			wantBounds: []float64{0, 10, 20},
		},
		{
			in: rm.Float64Histogram{
				Counts:  []uint64{1, 2},
				Buckets: []float64{5, 10, 20},
			},
			wantCounts: []uint64{0, 1, 2, 0},
			wantBounds: []float64{5, 10, 20},
		},
		{
			in: rm.Float64Histogram{
				Counts:  []uint64{1, 2},
				Buckets: []float64{5, 10, math.Inf(1)},
			},
			wantCounts: []uint64{0, 1, 2},
			wantBounds: []float64{5, 10},
		},
		{
			in: rm.Float64Histogram{
				Counts:  []uint64{1, 2, 3},
				Buckets: []float64{math.Inf(-1), 5, 10, math.Inf(1)},
			},
			wantCounts: []uint64{1, 2, 3},
			wantBounds: []float64{5, 10},
		},
	} {
		gotCounts, gotBounds := convertHistogram(&test.in)
		if g, w := gotCounts, test.wantCounts; !reflect.DeepEqual(g, w) {
			t.Errorf("%+v:\ncounts:\ngot  %v\nwant %v", test.in, g, w)
		}
		if g, w := gotBounds, test.wantBounds; !reflect.DeepEqual(g, w) {
			t.Errorf("%+v:\nbounds:\ngot  %v\nwant %v", test.in, g, w)
		}
	}
}
