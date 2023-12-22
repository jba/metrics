// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metricsdata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// type (
// 	S struct {
// 		Key string
// 		Value
// 	}

// 	Value struct {
// 		AsInt    int64   `json:"asInt,omitempty"`
// 		AsDouble float64 `json:"asDouble,omitempty"`
// 	}
// )

// func TestJSON(t *testing.T) {
// 	s := S{Key: "k", Value: Value{AsInt: 3}}
// 	out, err := json.MarshalIndent(s, "", "    ")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Printf("%s\n", out)
// }

func TestJSON(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	one := 1.0
	md := MetricsData{
		ResourceMetrics: []ResourceMetrics{
			{
				Resource: Resource{
					Attributes: []KeyValue{{Key: "service.name", Value: "my.service"}},
				},
				ScopeMetrics: []ScopeMetrics{{
					Scope: InstrumentationScope{
						Name:       "my.library",
						Version:    "1.0.0",
						Attributes: []KeyValue{{"my.scope.attribute", true}},
					},
					Metrics: []Metric{
						{
							Name:        "my.counter",
							Description: "I'm a Counter",
							Unit:        "1",
							Sum: &Sum{
								Temporality: TemporalityCumulative,
								IsMonotonic: true,
								DataPoints: []NumberDataPoint{
									{
										Number:     FloatNumber(5),
										Time:       time.Unix(0, 1544712660300000000),
										StartTime:  time.Unix(0, 1544712660300000000),
										Attributes: []KeyValue{{"my.counter.attr", "some value"}},
									},
								},
							},
						},
						{
							Name:        "my.gauge",
							Description: "I'm a Gauge",
							Unit:        "1",
							Gauge: &Gauge{
								DataPoints: []NumberDataPoint{
									{
										Number:     FloatNumber(10),
										Time:       time.Unix(0, 1544712660300000000),
										Attributes: []KeyValue{{"my.gauge.attr", int64(25)}},
									},
								},
							},
						},
						{
							Name:        "my.histogram",
							Description: "I'm a Histogram",
							Unit:        "1",
							Histogram: &Histogram{
								Temporality: TemporalityCumulative,
								DataPoints: []HistogramDataPoint{
									{
										Time:           time.Unix(0, 1544712660300000000),
										StartTime:      time.Unix(0, 1544712660300000000),
										Attributes:     []KeyValue{{"my.histogram.attr", "some value"}},
										BucketCounts:   []uint64{1, 1, 1},
										Sum:            3,
										Min:            &one,
										Max:            &one,
										ExplicitBounds: []float64{1},
									},
								},
							},
						},
					},
				}},
			},
		},
	}

	got, err := json.MarshalIndent(md, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// My editor insists on adding a newline at the end of "metrics.json".
	got = append(got, '\n')

	diff := cmp.Diff(want, got)
	if diff != "" {
		t.Errorf("mismatch (-want, +got):\n%s", diff)
	}
}
