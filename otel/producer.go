// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package otel connects the metrics package to OpenTelemetry.
package otel

import (
	"context"

	"github.com/jba/metrics"

	md "github.com/jba/metrics/metricsdata"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	ometric "go.opentelemetry.io/otel/sdk/metric"
	omd "go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Producer implements [go.opentelemetry.io/otel/sdk/metric.Producer].
// Create one with [NewProducer], then add it to a
// [go.opentelemetry.io/otel/sdk/metric.Reader]  using the
// [go.opentelemetry.io/otel/sdk/metric.WithProducer] option.
type Producer struct {
	scope instrumentation.Scope
	names []string
}

var _ ometric.Producer = (*Producer)(nil)

func NewProducer(scope instrumentation.Scope, metricNames ...string) *Producer {
	return &Producer{scope: scope, names: metricNames}
}

func (p *Producer) Produce(ctx context.Context) ([]omd.ScopeMetrics, error) {
	ms := metrics.Read(p.names...)
	sm := omd.ScopeMetrics{Scope: p.scope}
	for _, m := range ms {
		sm.Metrics = append(sm.Metrics, convertMetric(m))
	}
	return []omd.ScopeMetrics{sm}, nil
}

func convertMetric(m md.Metric) omd.Metrics {
	om := omd.Metrics{
		Name:        m.Name,
		Description: m.Description,
		Unit:        m.Unit,
	}
	switch {
	case m.Gauge != nil:
		if isInt(m.Gauge.DataPoints) {
			om.Data = omd.Gauge[int64]{
				DataPoints: mapSlice(m.Gauge.DataPoints, convertNumberDataPoint[int64]),
			}
		} else {
			om.Data = omd.Gauge[float64]{
				DataPoints: mapSlice(m.Gauge.DataPoints, convertNumberDataPoint[float64]),
			}
		}

	case m.Sum != nil:
		if isInt(m.Sum.DataPoints) {
			om.Data = omd.Sum[int64]{
				DataPoints:  mapSlice(m.Sum.DataPoints, convertNumberDataPoint[int64]),
				Temporality: omd.Temporality(m.Sum.Temporality),
				IsMonotonic: m.Sum.IsMonotonic,
			}
		} else {
			om.Data = omd.Sum[float64]{
				DataPoints:  mapSlice(m.Sum.DataPoints, convertNumberDataPoint[float64]),
				Temporality: omd.Temporality(m.Sum.Temporality),
				IsMonotonic: m.Sum.IsMonotonic,
			}
		}

	case m.Histogram != nil:
		t := omd.Temporality(m.Histogram.Temporality)
		if m.Histogram.IsInt {
			om.Data = omd.Histogram[int64]{
				Temporality: t,
				DataPoints:  mapSlice(m.Histogram.DataPoints, convertHistogramDataPoint[int64]),
			}
		} else {
			om.Data = omd.Histogram[float64]{
				Temporality: t,
				DataPoints:  mapSlice(m.Histogram.DataPoints, convertHistogramDataPoint[float64]),
			}
		}
	}
	return om
}

func isInt(dps []md.NumberDataPoint) bool {
	if len(dps) == 0 {
		return false // doesn't matter
	}
	return dps[0].Number.IsInt()
}

func convertNumberDataPoint[N int64 | float64](ndp md.NumberDataPoint) omd.DataPoint[N] {
	return omd.DataPoint[N]{
		Attributes: attribute.NewSet(mapSlice(ndp.Attributes, convertAttribute)...),
		StartTime:  ndp.StartTime,
		Time:       ndp.Time,
		Value:      ndp.Number.Value().(N),
	}
}

func convertHistogramDataPoint[N int64 | float64](hdp md.HistogramDataPoint) omd.HistogramDataPoint[N] {
	return omd.HistogramDataPoint[N]{
		Attributes:   attribute.NewSet(mapSlice(hdp.Attributes, convertAttribute)...),
		StartTime:    hdp.StartTime,
		Time:         hdp.Time,
		Count:        hdp.Count(),
		Bounds:       hdp.ExplicitBounds,
		BucketCounts: hdp.BucketCounts,
		Min:          ptrToExtrema[N](hdp.Min),
		Max:          ptrToExtrema[N](hdp.Max),
		Sum:          N(hdp.Sum),
	}
}

func ptrToExtrema[N int64 | float64](pf *float64) omd.Extrema[N] {
	if pf == nil {
		return omd.Extrema[N]{}
	}
	return omd.NewExtrema[N](N(*pf))
}

func convertAttribute(kv md.KeyValue) attribute.KeyValue {
	var val attribute.Value
	switch v := kv.Value.(type) {
	case string:
		val = attribute.StringValue(v)
	case bool:
		val = attribute.BoolValue(v)
	case int64:
		val = attribute.Int64Value(v)
	default:
		panic("bad Value type")
	}
	return attribute.KeyValue{
		Key:   attribute.Key(kv.Key),
		Value: val,
	}
}

func mapSlice[E1, E2 any](s []E1, f func(E1) E2) []E2 {
	if s == nil {
		return nil
	}
	r := make([]E2, len(s))
	for i, e := range s {
		r[i] = f(e)
	}
	return r
}
