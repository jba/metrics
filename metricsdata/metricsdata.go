// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metricsdata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

////////////////////////////////////////////////////////////////
// OTLP types.
// These cover a subset the OTLP protos and serialize to the the JSON format.
// See https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/metrics/v1/metrics.proto.
// and https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding

type MetricsData struct {
	ResourceMetrics []ResourceMetrics `json:"resourceMetrics"`
}

type ResourceMetrics struct {
	Resource     Resource
	ScopeMetrics []ScopeMetrics
}

type ScopeMetrics struct {
	Scope   InstrumentationScope
	Metrics []Metric
}

type Resource struct {
	Attributes []KeyValue
}

type InstrumentationScope struct {
	Name       string
	Version    string
	Attributes []KeyValue
}

type Metric struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	// oneof
	Gauge     *Gauge     `json:"gauge,omitempty"`
	Sum       *Sum       `json:"sum,omitempty"`
	Histogram *Histogram `json:"histogram,omitempty"`
	// not doing ExponentialHistogram or Summary
}

type Gauge struct {
	DataPoints []NumberDataPoint `json:"dataPoints"`
}

type Sum struct {
	DataPoints             []NumberDataPoint `json:"dataPoints"`
	AggregationTemporality int               // TODO
	IsMonotonic            bool
}

type NumberDataPoint struct {
	Attributes        []KeyValue
	StartTimeUnixNano int64
	TimeUnixNano      int64
	Number            Number
}

func (n NumberDataPoint) MarshalJSON() ([]byte, error) {
	fs := []jsonField{
		{"attributes", n.Attributes},
		{"timeUnixNano", n.TimeUnixNano},
		{"", nil},
		{"", nil},
	}
	if n.Number.isInt {
		fs[2] = jsonField{"asInt", n.Number}
	} else {
		fs[2] = jsonField{"asDouble", n.Number}
	}
	if n.StartTimeUnixNano != 0 {
		fs[3] = jsonField{"startTimeUnixNano", n.StartTimeUnixNano}
	}
	return marshalJSONObject(fs)
}

type Number struct {
	isInt bool
	i     int64
}

func IntNumber(i int64) Number {
	return Number{isInt: true, i: i}
}

func FloatNumber(f float64) Number {
	return Number{isInt: false, i: int64(math.Float64bits(f))}
}

func (n Number) MarshalJSON() ([]byte, error) {
	if n.isInt {
		return json.Marshal(n.i)
	}
	return json.Marshal(math.Float64frombits(uint64(n.i)))
}

type Histogram struct {
	DataPoints             []HistogramDataPoint `json:"dataPoints"`
	AggregationTemporality int                  // TODO
}

type HistogramDataPoint struct {
	Attributes        []KeyValue
	StartTimeUnixNano int64
	TimeUnixNano      int64
	//Count             uint64 // sum of BucketCounts; can we omit?
	BucketCounts []uint64

	// The boundaries for bucket at index i are:
	//
	// (-infinity, explicit_bounds[i]] for i == 0
	// (explicit_bounds[i-1], explicit_bounds[i]] for 0 < i < size(explicit_bounds)
	// (explicit_bounds[i-1], +infinity) for i == size(explicit_bounds)
	//
	// The values in the explicit_bounds array must be strictly increasing.
	//
	// Histogram buckets are inclusive of their upper boundary, except the last
	// bucket where the boundary is at infinity. This format is intentionally
	// compatible with the OpenMetrics histogram definition.
	ExplicitBounds []float64
}

type KeyValue struct {
	Key   string `json:"key"`
	Value any    // one of string, bool, int64
}

func (kv KeyValue) MarshalJSON() ([]byte, error) {
	var valkey string
	switch kv.Value.(type) {
	case int64:
		valkey = "intValue"
	case bool:
		valkey = "boolValue"
	case string:
		valkey = "stringValue"
	default:
		return nil, fmt.Errorf("bad value type in KeyValue: %T", kv.Value)
	}
	return marshalJSONObject([]jsonField{
		{"key", kv.Key},
		{valkey, kv.Value},
	})
}

type jsonField struct {
	name  string
	value any
}

func marshalJSONObject(fields []jsonField) ([]byte, error) {
	buf := make([]byte, 0, 128)
	buf = append(buf, '{')
	for i, f := range fields {
		if f.name == "" {
			continue
		}
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, strconv.Quote(f.name)...)
		buf = append(buf, ':')
		data, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		buf = append(buf, data...)
	}
	return append(buf, '}'), nil
}
