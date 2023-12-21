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
	return &Producer{names: metricNames}
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
				DataPoints: convertNumberDataPoints[int64](m.Gauge.DataPoints),
			}
		} else {
			om.Data = omd.Gauge[float64]{
				DataPoints: convertNumberDataPoints[float64](m.Gauge.DataPoints),
			}
		}
	case m.Sum != nil:
		if isInt(m.Sum.DataPoints) {
			om.Data = omd.Sum[int64]{
				DataPoints:  convertNumberDataPoints[int64](m.Sum.DataPoints),
				Temporality: omd.Temporality(m.Sum.Temporality),
				IsMonotonic: m.Sum.IsMonotonic,
			}
		} else {
			om.Data = omd.Sum[float64]{
				DataPoints:  convertNumberDataPoints[float64](m.Sum.DataPoints),
				Temporality: omd.Temporality(m.Sum.Temporality),
				IsMonotonic: m.Sum.IsMonotonic,
			}
		}

	case m.Histogram != nil:
		panic("unimp")
	}
	return om
}

func isInt(dps []md.NumberDataPoint) bool {
	if len(dps) == 0 {
		return false // doesn't matter
	}
	return dps[0].Number.IsInt()
}

func convertNumberDataPoints[N int64 | float64](ndps []md.NumberDataPoint) []omd.DataPoint[N] {
	var dps []omd.DataPoint[N]
	for _, ndp := range ndps {
		dp := omd.DataPoint[N]{
			Attributes: convertAttributes(ndp.Attributes),
			StartTime:  ndp.StartTime,
			Time:       ndp.Time,
			Value:      md.NumberValue[N](ndp.Number),
		}
		dps = append(dps, dp)
	}
	return dps
}

func convertAttributes(kvs []md.KeyValue) attribute.Set {
	var akvs []attribute.KeyValue
	for _, kv := range kvs {
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
		akvs = append(akvs, attribute.KeyValue{
			Key:   attribute.Key(kv.Key),
			Value: val,
		})
	}
	return attribute.NewSet(akvs...)
}

// func (m Metric) IsInt() bool {
// 	switch {
// 	case m.Gauge != nil:
// 		return isInt(m.Gauge.DataPoints)
// 	case m.Sum != nil:
// 		return isInt(m.Sum.DataPoints)
// 	case m.Histogram != nil:
// 		// Histograms are always floats.
// 		return false
// 	default: // shouldn't happen
// 		return false
// 	}
// }
