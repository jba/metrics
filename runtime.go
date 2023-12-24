// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"math"
	rm "runtime/metrics"
	"strings"

	md "github.com/jba/metrics/metricsdata"
)

type runtimeReader struct {
	descs        []Description
	descsByName  map[string]Description
	nameToRMName map[string]string
}

func initRuntimeMetrics() {
	var ds []Description
	dbn := map[string]Description{}
	nameToRMName := map[string]string{}
	for _, rd := range rm.All() {
		d := translateDescription(rd)
		ds = append(ds, d)
		dbn[d.Name] = d
		nameToRMName[d.Name] = rd.Name
	}
	Register(runtimeReader{ds, dbn, nameToRMName})
}

func translateDescription(rd rm.Description) Description {
	name, unit := splitRuntimeMetricsName(rd.Name)
	var sk SumKind
	if rd.Cumulative || !nonSum[rd.Name] {
		sk = Summable
	} else {
		sk = NonSummable
	}
	return Description{
		Name:        "runtime" + name,
		Unit:        unit,
		Description: rd.Description,
		Cumulative:  rd.Cumulative,
		Sum:         sk,
	}
}

func splitRuntimeMetricsName(rmName string) (name, unit string) {
	switch rmName {
	case "/gc/heap/allocs:bytes":
		return "/gc/heap/allocs/bytes", ""
	case "/gc/heap/allocs:objects":
		return "/gc/heap/allocs/objects", ""
	case "/gc/heap/frees:bytes":
		return "/gc/heap/frees/bytes", ""
	case "/gc/heap/frees:objects":
		return "/gc/heap/frees/objects", ""
	default:
		name, unit, _ = strings.Cut(rmName, ":")
		return name, unit
	}
}

func (r runtimeReader) Descriptions() []Description {
	return r.descs
}

func (r runtimeReader) Read(names []string) []md.Metric {
	samples := make([]rm.Sample, len(names))
	for i, n := range names {
		rn := r.nameToRMName[n]
		if rn != "" { // ignore wrong names (TODO: error?)
			samples[i].Name = rn
		}
	}
	rm.Read(samples)
	var ms []md.Metric
	for i, s := range samples {
		d := r.descsByName[names[i]]
		ms = append(ms, sampleToMetric(d, s))
	}
	return ms
}

func sampleToMetric(d Description, s rm.Sample) md.Metric {
	if s.Value.Kind() != rm.KindFloat64Histogram {
		dp := md.NumberDataPoint{
			Number: valueToNumber(s.Value),
		}
		return newNumberMetric(d, []md.NumberDataPoint{dp})
	}
	counts, bounds := convertHistogram(s.Value.Float64Histogram())
	return md.Metric{
		Name:        d.Name,
		Description: d.Description,
		Unit:        d.Unit,
		Histogram: &md.Histogram{
			DataPoints: []md.HistogramDataPoint{
				{
					BucketCounts:   counts,
					ExplicitBounds: bounds,
				},
			},
		},
	}
}

func valueToNumber(v rm.Value) md.Number {
	switch v.Kind() {
	case rm.KindFloat64:
		return md.FloatNumber(v.Float64())
	case rm.KindUint64:
		// TODO: check overflow
		return md.IntNumber(int64(v.Uint64()))
	default:
		panic("bad value kind")
	}
}

// TODO: for integer buckets like sizeClass, subtract/add one
// to get the inclusiveness to work.
// We have to know this based on the metric name.
func convertHistogram(h *rm.Float64Histogram) (counts []uint64, bounds []float64) {
	// Ignore the closedness of the boundaries; what else can we do?
	bounds = h.Buckets
	addUnderflowCount := true
	addOverflowCount := true
	if bounds[0] == math.Inf(-1) {
		// OTel represents -infinity implicitly (it is always the lower
		// bound of the first bucket), so drop the first bound.
		// We won't need an initial count for the underflow.
		bounds = bounds[1:]
		addUnderflowCount = false
	}
	if bounds[len(bounds)-1] == math.Inf(1) {
		// OTel represents +infinity implicitly (it is always the upper
		// bound of the last bucket), so drop the last bound.
		// We won't need a final count for the overflow.
		bounds = bounds[:len(bounds)-1]
		addOverflowCount = false
	}
	nc := len(h.Counts)
	off := 0
	if addUnderflowCount {
		nc++
		off = 1
	}
	if addOverflowCount {
		nc++
	}
	counts = make([]uint64, nc)
	copy(counts[off:], h.Counts)
	return counts, bounds
}

// non-cumulative metrics where data points from different
// time series cannot be summed together.
var nonSum = map[string]bool{
	"/gc/gogc:percent":                  true,
	"/gc/gomemlimit:bytes":              true,
	"/gc/heap/goal:bytes":               true,
	"/gc/limiter/last-enabled:gc-cycle": true,
}
