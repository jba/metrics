// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metricsdata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
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
	Resource     Resource       `json:"resource"`
	ScopeMetrics []ScopeMetrics `json:"scopeMetrics"`
}

type ScopeMetrics struct {
	Scope   InstrumentationScope `json:"scope"`
	Metrics []Metric             `json:"metrics"`
}

type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

type InstrumentationScope struct {
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Attributes []KeyValue `json:"attributes"`
}

type Metric struct {
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
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
	Temporality int               `json:"aggregationTemporality"`
	IsMonotonic bool              `json:"isMonotonic"`
	DataPoints  []NumberDataPoint `json:"dataPoints"`
}

const (
	TemporalityUndefined = iota
	TemporalityCumulative
	TemporalityDelta
)

type NumberDataPoint struct {
	Number     Number
	Time       time.Time
	StartTime  time.Time
	Attributes []KeyValue
}

func (n NumberDataPoint) MarshalJSON() ([]byte, error) {
	fs := []jsonField{
		{"", nil},
		{"", nil},
		{"timeUnixNano", n.Time.UnixNano()},
		{"attributes", n.Attributes},
	}
	if n.Number.isInt {
		fs[0] = jsonField{"asInt", n.Number}
	} else {
		fs[0] = jsonField{"asDouble", n.Number}
	}
	if !n.StartTime.IsZero() {
		fs[1] = jsonField{"startTimeUnixNano", n.StartTime.UnixNano()}
	}
	return marshalJSONObject(fs)
}

type Number struct {
	isInt bool
	i     int64
}

func NumberValue[N int64 | float64](n Number) N {
	// TODO: validate
	return N(n.i)
}

func IntNumber(i int64) Number {
	return Number{isInt: true, i: i}
}

func FloatNumber(f float64) Number {
	return Number{isInt: false, i: int64(math.Float64bits(f))}
}

func (n Number) IsInt() bool { return n.isInt }

func (n Number) MarshalJSON() ([]byte, error) {
	if n.isInt {
		return json.Marshal(n.i)
	}
	return json.Marshal(math.Float64frombits(uint64(n.i)))
}

type Histogram struct {
	Temporality int                  `json:"aggregationTemporality"` // TODO
	DataPoints  []HistogramDataPoint `json:"dataPoints"`
}

type HistogramDataPoint struct {
	Attributes   []KeyValue
	Time         time.Time
	StartTime    time.Time
	BucketCounts []uint64
	Sum          float64
	Min, Max     float64
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

type jsonHDP struct {
	StartTime      int64      `json:"startTimeUnixNano,omitEmpty"`
	Time           int64      `json:"timeUnixNano"`
	Count          uint64     `json:"count"`
	Sum            float64    `json:"sum"`
	BucketCounts   []uint64   `json:"bucketCounts"`
	ExplicitBounds []float64  `json:"explicitBounds"`
	Min            float64    `json:"min"`
	Max            float64    `json:"max"`
	Attributes     []KeyValue `json:"attributes"`
}

func (hdp HistogramDataPoint) MarshalJSON() ([]byte, error) {
	count := uint64(0)
	for _, c := range hdp.BucketCounts {
		count += c
	}
	jh := jsonHDP{
		Attributes:     hdp.Attributes,
		Time:           hdp.Time.UnixNano(),
		StartTime:      hdp.StartTime.UnixNano(),
		Count:          count,
		Sum:            hdp.Sum,
		Min:            hdp.Min,
		Max:            hdp.Max,
		BucketCounts:   hdp.BucketCounts,
		ExplicitBounds: hdp.ExplicitBounds,
	}
	return json.Marshal(jh)
}

type KeyValue struct {
	Key   string
	Value any
}

type jsonValue struct {
	Key   string `json:"key"`
	Value struct {
		S *string `json:"stringValue,omitempty"`
		B *bool   `json:"boolValue,omitempty"`
		I *int64  `json:"intValue,omitempty"`
	} `json:"value"`
}

func (kv KeyValue) MarshalJSON() ([]byte, error) {
	jv := jsonValue{Key: kv.Key}
	switch v := kv.Value.(type) {
	case int64:
		jv.Value.I = &v
	case bool:
		jv.Value.B = &v
	case string:
		jv.Value.S = &v
	default:
		return nil, fmt.Errorf("bad value type in KeyValue: %T", kv.Value)
	}
	return json.Marshal(jv)
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
