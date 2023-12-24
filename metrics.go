// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	md "github.com/jba/metrics/metricsdata"
)

var (
	mu           sync.Mutex
	names        map[string]bool
	readers      []Reader
	errorHandler = func(err error) {
		fmt.Fprintln(os.Stderr, err)
	}
)

// Read reads the metrics for which f returns true.
// / If f is nil, it reads all registered metrics.
func Read(f func(name string) bool) []md.Metric {
	rss := selectReaders(f)
	return readMetrics(time.Now(), rss)
}

// NewHandler returns an http.Handler that serves the metrics
// for which f returns true.
//
// By default, the handler serves the [JSON encoding] of
// [OTLP], the Open Telemetry metrics protocol. With the query
// parameter "format=prometheus", it serves the [prometheus protocol].
//
// When serving OTLP, the resource argument populates that protocol's Resource
// message. It may be nil.
//
// [JSON encoding]: https://opentelemetry.io/docs/specs/otlp/#json-protobuf-encoding
// [OTLP]: https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/metrics/v1/metrics.proto
// [prometheus protocol]: TODO
func NewHandler(resource map[string]string, f func(name string) bool) http.Handler {
	// TODO: prometheus protocol
	rss := selectReaders(f)

	var rattrs []md.KeyValue
	for k, v := range resource {
		rattrs = append(rattrs, md.KeyValue{Key: k, Value: v})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms := readMetrics(time.Now(), rss)
		md := md.MetricsData{
			ResourceMetrics: []md.ResourceMetrics{{
				Resource: md.Resource{Attributes: rattrs},
				ScopeMetrics: []md.ScopeMetrics{{
					Scope: md.InstrumentationScope{
						Name:    "go",
						Version: runtime.Version(),
					},
					Metrics: ms,
				}},
			}},
		}

		data, err := json.MarshalIndent(md, "", "    ")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(data)
		fmt.Fprintln(w)
	})
}

type readerSelected struct {
	r     Reader
	names []string
}

// Note: we can't use a map from name to Reader in order to quickly
// find the list of Readers, because we can't be sure a Reader is comparable.

func selectReaders(f func(string) bool) []readerSelected {
	if f == nil {
		f = func(string) bool { return true }
	}
	var rs []readerSelected
	mu.Lock()
	defer mu.Unlock()
	for _, r := range readers {
		var names []string
		for _, d := range r.Descriptions() {
			if f(d.Name) {
				names = append(names, d.Name)
			}
		}
		if len(names) > 0 {
			rs = append(rs, readerSelected{r, names})
		}
	}
	return rs
}

func readMetrics(t time.Time, rss []readerSelected) []md.Metric {
	var ms []md.Metric
	for _, rs := range rss {
		ms = append(ms, rs.r.Read(rs.names)...)
	}
	for _, m := range ms {
		setNonZeroTimes(t, m)
	}
	return ms
}

func setNonZeroTimes(t time.Time, m md.Metric) {
	switch {
	case m.Gauge != nil:
		setNonZeroTimesNumber(t, m.Gauge.DataPoints)
	case m.Sum != nil:
		setNonZeroTimesNumber(t, m.Sum.DataPoints)
	case m.Histogram != nil:
		setNonZeroTimesHistogram(t, m.Histogram.DataPoints)
	}
}

func setNonZeroTimesNumber(t time.Time, dps []md.NumberDataPoint) {
	for i, dp := range dps {
		if dp.Time.IsZero() {
			dps[i].Time = t
		}
	}
}

func setNonZeroTimesHistogram(t time.Time, dps []md.HistogramDataPoint) {
	for i, dp := range dps {
		if dp.Time.IsZero() {
			dps[i].Time = t
		}
	}
}

// SumKind describes whether it makes sense to add metric values together. If a
// metric is Summable, then its values may be meaningfully added together. For
// example, a metric tracking the number of allocated bytes can be added across
// multiple processes or machines to produce a meaningful total.
//
// A NonSummable metric cannot be meaningfully added.
// The percentage of CPU used by a process is one example of such a metric.
type SumKind int

const (
	NonSummable SumKind = iota
	Summable
)

// Description describes a metric.
type Description struct {
	Name        string
	Description string
	Unit        string
	Cumulative  bool
	Sum         SumKind
}

// All returns descriptions for all registered metrics.
func All() []Description {
	mu.Lock()
	defer mu.Unlock()
	var ds []Description
	for _, r := range readers {
		ds = append(ds, r.Descriptions()...)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })
	return ds
}

func (d *Description) toMetric() md.Metric {
	return md.Metric{
		Name:        d.Name,
		Description: d.Description,
		Unit:        d.Unit,
	}
}

func newNumberMetric(d Description, ndps []md.NumberDataPoint) md.Metric {
	if len(ndps) == 0 {
		panic("no points")
	}
	m := md.Metric{
		Name:        d.Name,
		Description: d.Description,
		Unit:        d.Unit,
	}
	switch d.Sum {
	case Summable:
		m.Sum = &md.Sum{
			Temporality: md.TemporalityCumulative,
			IsMonotonic: d.Cumulative,
			DataPoints:  ndps,
		}
	case NonSummable:
		m.Gauge = &md.Gauge{DataPoints: ndps}
	default:
		panic("bad SumKind")
	}
	return m
}

// A Reader reads values for one or more metrics.
type Reader interface {
	// Descriptions returns a Description for each metric
	// that the Reader can read.
	Descriptions() []Description

	// Read returns the Metrics with the given names.
	//
	// The caller will populate the zero times of the returned metric data
	// points with the time of the call. A Reader may set a different time for
	// events known to have occurred earlier.
	Read(names []string) []md.Metric
}

// Register records the reader in the set of metrics.
// It panics if any of the metric names were already registered.
func Register(r Reader) {
	mu.Lock()
	defer mu.Unlock()
	for _, d := range r.Descriptions() {
		if names[d.Name] {
			panic(fmt.Sprintf("duplicate metric %q", d.Name))
		}
		names[d.Name] = true
	}
	readers = append(readers, r)
}

// Reset metric system to initial state.
// Useful for testing.
func Reset() {
	mu.Lock()
	names = map[string]bool{}
	readers = nil
	mu.Unlock()
	initRuntimeMetrics()
}

func init() {
	Reset()
}

////////////////////////////////////////////////////////////////
// Convenience API.

// SetErrorHandler sets a function to be called when
// an error happens while recording a metric.
// It returns the previous value.
//
// The default error handler prints the error to stderr.
func SetErrorHandler(f func(error)) func(error) {
	mu.Lock()
	defer mu.Unlock()
	old := errorHandler
	errorHandler = f
	return old
}

// A Counter is a cumulative count that is always increasing.
type Counter[N ~int64 | ~float64] struct {
	commonInstrument[N]
}

// NewCounter creates and registers a Counter.
//
// The corresponding metric has the given name and description, and is
// cumulative and summable. If N is time.Duration, the unit will be
// "ns". If N has a method
//
//	Unit() string
//
// then the unit will be the result of calling that method on a zero
// value of N. Otherwise, the unit will be the empty string.
func NewCounter[N ~int64 | ~float64](name, desc string) *Counter[N] {
	c := &Counter[N]{}
	c.init()
	ig := &numberReader[N]{
		desc: newDescription[N](name, desc, true, Summable),
		ni:   c,
	}
	Register(ig)
	return c
}

// Add adds n to the value of the counter.
// If n is negative, NaN, or infinity, it is not added and
// the error handler is called with an appropriate error.
func (c *Counter[N]) Add(n N) {
	if n < 0 {
		errorf("metrics.Counter.Add: negative value: %v", n)
	} else if c.checkFloat("metrics.Counter.Add", n) {
		c.value.Add(n)
	}
}

// A Gauge records a value at a single point in time.
// The value of a Gauge may go up or down, and is not
// cumulative.
type Gauge[N ~int64 | ~float64] struct {
	commonInstrument[N]
}

// NewGauge creates and registers a Gauge.
//
// The corresponding metric has the given name, description and SumKind.
// It is not cumulative.
// If N is time.Duration, the unit will be "ns". If N has a method
//
//	Unit() string
//
// then the unit will be the result of calling that method on a zero
// value of N.
// Otherwise, the unit will be the empty string.
func NewGauge[N ~int64 | ~float64](name string, sum SumKind, desc string) *Gauge[N] {
	g := &Gauge[N]{}
	g.init()
	nr := &numberReader[N]{
		desc: newDescription[N](name, desc, false, sum),
		ni:   g,
	}
	Register(nr)
	return g
}

// Set sets the value of the gauge to n.
// If n is NaN, or infinity, the gauge's value is left unchanged
// and the error handler is called with an appropriate error.
func (g *Gauge[N]) Set(n N) {
	// TODO: should we record the time here and report it in Read?
	if g.checkFloat("metrics.Gauge.Set", n) {
		g.value.Store(n)
	}
}

type commonInstrument[N ~int64 | ~float64] struct {
	value   atomicNumber[N]
	isFloat bool
}

func (c *commonInstrument[N]) init() {
	c.value = newAtomicNumber[N]()
	c.isFloat = isFloat[N]()
}

func (c *commonInstrument[N]) checkFloat(prefix string, n N) bool {
	if c.isFloat {
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, +1) || math.IsInf(f, -1) {
			errorf("%s: bad floating-point value: %f", prefix, f)
			return false
		}
	}
	return true
}

func (c *commonInstrument[N]) read() md.NumberDataPoint {
	return md.NumberDataPoint{
		Number: newNumber(c.value.Load()),
	}
}

type observableGauge[N ~int64 | ~float64] struct {
	observe func() N
}

// RegisterObservableGauge registers a new observable gauge
// that gets its value by calling the given function.
//
// The corresponding metric has the given name, description and SumKind.
// It is not cumulative.
// If N is time.Duration, the unit will be "ns". If N has a method
//
//	Unit() string
//
// then the unit will be the result of calling that method on a zero
// value of N.
// Otherwise, the unit will be the empty string.
func RegisterObservableGauge[N ~int64 | ~float64](name string, sum SumKind, observe func() N, desc string) {
	g := &observableGauge[N]{observe: observe}
	nr := &numberReader[N]{
		desc: newDescription[N](name, desc, false, sum),
		ni:   g,
	}
	Register(nr)
}

func (g *observableGauge[N]) read() md.NumberDataPoint {
	return md.NumberDataPoint{
		Number: newNumber(g.observe()),
	}
}

////////////////////////////////////////////////////////////////
// Groups

// A Group is a collection of instruments, each with a different
// value of Attrs. Attrs must be a struct.
type Group[I any, Attrs comparable] struct {
	m   *syncMap[Attrs, I]
	new func() I
}

// At returns the instrument for the given attrs, creating one if it
// does not exist.
func (g Group[I, A]) At(attrs A) I {
	inst, ok := g.m.Load(attrs)
	if !ok {
		// Use LoadOrStore to avoid races:
		// - If no other goroutine has called intern since the above Load, then
		//   the map will not contain a value for key, and v will be m.new().
		// - If another goroutine G executed the LoadOrStore before we did but after
		//   we called Load above, v will be the value stored by G, and the value
		//   returned by m.new will be dropped.
		inst, _ = g.m.LoadOrStore(attrs, g.new())
	}
	return inst
}

// NewCounterGroup creates a group of counters that differ in the
// values of the type A, which must be a struct.
// See [NewCounter] for details about the corresponding metric.
func NewCounterGroup[N ~int64 | ~float64, A comparable](name, desc string) Group[*Counter[N], A] {
	return newNumberGroup[*Counter[N], A](
		newDescription[N](name, desc, true, Summable),
		func() *Counter[N] {
			var c Counter[N]
			c.init()
			return &c
		})
}

// NewGaugeGroup creates a group of gauges that differ in the
// values of the type A, which must be a struct.
// See [NewGauge] for details about the corresponding metric.
func NewGaugeGroup[N ~int64 | ~float64, A comparable](name string, sum SumKind, desc string) Group[*Gauge[N], A] {
	return newNumberGroup[*Gauge[N], A](
		newDescription[N](name, desc, false, sum),
		func() *Gauge[N] {
			var g Gauge[N]
			g.init()
			return &g
		})
}

func newNumberGroup[I numberInstrument, A comparable](d Description, new func() I) Group[I, A] {
	m := &syncMap[A, I]{}
	gnr := &groupNumberReader[I, A]{
		desc:          d,
		makeKeyValues: keyValueMaker[A](),
		instruments:   m,
	}
	Register(gnr)
	return Group[I, A]{m: m, new: new}
}

// An ObservableGaugeGroup is a group of observable gauges.
type ObservableGaugeGroup[N ~int64 | ~float64, A comparable] struct {
	m *syncMap[A, *observableGauge[N]]
}

// NewObservableGaugeGroup creates an ObservableGaugeGroup.
// See [RegisterObservableGauge] for details about the corresponding metric.
func NewObservableGaugeGroup[N ~int64 | ~float64, A comparable](name string, sum SumKind, desc string) ObservableGaugeGroup[N, A] {
	m := &syncMap[A, *observableGauge[N]]{}
	gnr := &groupNumberReader[*observableGauge[N], A]{
		desc:          newDescription[N](name, desc, false, sum),
		makeKeyValues: keyValueMaker[A](),
		instruments:   m,
	}
	Register(gnr)
	return ObservableGaugeGroup[N, A]{m: m}
}

// Register adds a new observable gauge to the group.
// If the value of attrs is already registered, the given value
// is ignored and the error handler is called.
func (g ObservableGaugeGroup[N, A]) Register(attrs A, observe func() N) {
	og := &observableGauge[N]{observe: observe}
	if _, found := g.m.LoadOrStore(attrs, og); found {
		errorf("metrics.ObservableGaugeGroup.Register: duplicate attributes: %+v", attrs)
	}
}

////////////////////////////////////////////////////////////////
// Histograms.

// A Histogram represents a distribution of values.
type Histogram[N ~int64 | ~float64] struct {
	counts []atomic.Uint64
	bounds []N
}

// NewHistogram creates and registers a histogram with the given bounds.
// For each i, bounds[i] is the upper bound of bucket i.
// There is one additional overflow bucket for values greater than the last bound.
// In other words, a value x belongs in bucket i if:
//
//	x <= bounds[i]               for i == 0
//	bounds[i-1] < x <= bounds[i] for 0 < i < len(bounds)
//	bounds[i-1] < x              for i == len(bounds)
//
// The corresponding metric has the given name and description, and is
// cumulative and summable. If N is time.Duration, the unit will be
// "ns". If N has a method
//
//	Unit() string
//
// then the unit will be the result of calling that method on a zero
// value of N. Otherwise, the unit will be the empty string.
func NewHistogram[N ~int64 | ~float64](name string, bounds []N, desc string) *Histogram[N] {
	validateBounds(bounds)
	h := newHistogram(bounds)
	hr := &histogramReader[N, struct{}]{
		desc:           newDescription[N](name, desc, true, Summable),
		explicitBounds: floatBounds(bounds),
		makeKeyValues:  func(struct{}) []md.KeyValue { return nil },
		histdata:       func(f func(struct{}, []atomic.Uint64)) { f(struct{}{}, h.counts) },
	}
	Register(hr)
	return h
}

func floatBounds[N ~int64 | ~float64](bounds []N) []float64 {
	fbs := make([]float64, len(bounds))
	for i, b := range bounds {
		fbs[i] = float64(b)
	}
	return fbs
}

func newHistogram[N ~int64 | ~float64](bounds []N) *Histogram[N] {
	counts := make([]atomic.Uint64, len(bounds)+1)
	return &Histogram[N]{
		counts: counts,
		bounds: bounds,
	}
}

func validateBounds[N ~int64 | ~float64](bounds []N) {
	if len(bounds) == 0 {
		panic("no bounds")
	}
	prev := bounds[0]
	for _, b := range bounds[1:] {
		if prev >= b {
			panicf("bound %v is not less than following bound %v", prev, b)
		}
	}
}

// Record records a value in the histogram by incrementing
// the count of the bucket containing it.
func (h *Histogram[N]) Record(x N) {
	i := bucketIndex(x, h.bounds)
	h.counts[i].Add(1)
}

func bucketIndex[N ~int64 | ~float64](value N, bounds []N) int {
	//	(-infinity, bounds[i]] for i == 0
	//	(bounds[i-1], bounds[i]] for 0 < i < len(bounds)
	//	(bounds[i-1], +infinity) for i == len(bounds)
	for i, b := range bounds {
		if value <= b {
			return i
		}
	}
	return len(bounds)
}

// NewHistogram creates a group of Histograms, each with a different
// value of the type A.
// See [NewHistogram] for details about the corresponding metric.
func NewHistogramGroup[N ~int64 | ~float64, A comparable](name string, bounds []N, desc string) Group[*Histogram[N], A] {
	validateBounds(bounds)
	m := &syncMap[A, *Histogram[N]]{}
	hr := &histogramReader[N, A]{
		desc:           newDescription[N](name, desc, true, Summable),
		explicitBounds: floatBounds(bounds),
		makeKeyValues:  keyValueMaker[A](),
		histdata: func(f func(A, []atomic.Uint64)) {
			m.Range(func(a A, h *Histogram[N]) bool {
				f(a, h.counts)
				return true
			})
		},
	}
	Register(hr)
	return Group[*Histogram[N], A]{
		m: m,
		new: func() *Histogram[N] {
			return newHistogram(bounds)
		},
	}
}

type histogramReader[N ~int64 | ~float64, A comparable] struct {
	desc           Description
	explicitBounds []float64
	makeKeyValues  func(A) []md.KeyValue
	histdata       func(func(A, []atomic.Uint64))
}

func (r *histogramReader[N, A]) Descriptions() []Description {
	return []Description{r.desc}
}

func (r *histogramReader[N, A]) Read([]string) []md.Metric {
	var dps []md.HistogramDataPoint
	r.histdata(func(a A, acs []atomic.Uint64) {
		counts := make([]uint64, len(acs))
		for i, c := range acs {
			counts[i] = c.Load()
		}
		dp := md.HistogramDataPoint{
			Attributes:     r.makeKeyValues(a),
			BucketCounts:   counts,
			ExplicitBounds: r.explicitBounds,
		}
		dps = append(dps, dp)
	})
	return []md.Metric{{
		Name:        r.desc.Name,
		Description: r.desc.Description,
		Unit:        r.desc.Unit,
		Histogram: &md.Histogram{
			Temporality: md.TemporalityCumulative,
			IsInt:       !isFloat[N](),
			DataPoints:  dps,
		},
	}}
}

// LinearBounds creates a set of bounds that establish equal-sized
// buckets of the given size from min to max.
//
// For example, LinearBounds[int64](5, 2, 12) returns
//
//	[]int64{2, 7, 12}
//
// which results in buckets with ranges
//
//	(-infinity, 2]
//	(2, 7]
//	(7, 12]
//	(12, +infinity)
func LinearBounds[N ~int64 | ~float64](size, min, max N) []N {
	var bounds []N
	for b := N(min); b <= max; b += size {
		bounds = append(bounds, b)
	}
	return bounds
}

// ExponentialBounds creates a set of bounds that establish buckets
// whose size increases exponentially.
// The argument "first", which must not be negative,
// is the upper bound of the first bucket.
// The upper bound of each subsequent bucket is base times the
// upper bound of the previous. base must be greater than 1.
// The upper bound of the last bucket will not exceed max.
//
// For example, ExponentialBounds[int64](10, 1, 100) returns
//
//	[]int64{1, 10, 100}
//
// which results in buckets with ranges
//
//	(-infinity, 1]
//	(1, 10]
//	(10, 100]
//	(100, +infinity)
//
// As a special case, if first is 0, the first bound will be 0
// and the second will be 1.
func ExponentialBounds[N ~float64 | ~int64](base, first, max N) []N {
	if base <= 1 {
		panic("metrics.ExponentialBounds: base must be greater than 1")
	}
	if first < 0 {
		panic("metrics.ExponentialBounds: first must be non-negative")
	}
	bounds := []N{first}
	var b N
	if first == 0 {
		b = 1
	} else {
		b = first
	}
	for b <= max {
		bounds = append(bounds, b)
		b *= base
	}
	return bounds
}

////////////////////////////////////////////////////////////////
// Support

func newDescription[N number](name, desc string, cum bool, sum SumKind) Description {
	return Description{
		Name:        name,
		Description: desc,
		Unit:        unit[N](),
		Cumulative:  cum,
		Sum:         sum,
	}
}

type numberInstrument interface {
	read() md.NumberDataPoint
}

type numberReader[N ~int64 | ~float64] struct {
	desc Description
	ni   numberInstrument
}

func (r *numberReader[N]) Descriptions() []Description {
	return []Description{r.desc}
}

func (r *numberReader[N]) Read([]string) []md.Metric {
	// TODO: what if names are wrong?
	dp := r.ni.read()
	return []md.Metric{newNumberMetric(r.desc, []md.NumberDataPoint{dp})}
}

func unit[N ~int64 | ~float64]() string {
	var z N
	if _, ok := any(z).(time.Duration); ok {
		return "ns"
	}
	if u, ok := any(z).(interface {
		Unit() string
	}); ok {
		return u.Unit()
	}
	return ""
}

func newNumber[N ~int64 | ~float64](n N) md.Number {
	if !isFloat[N]() {
		return md.IntNumber(int64(n))
	}
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, +1) || math.IsInf(f, -1) {
		errorf("metrics: bad floating-point value: %f", f)
		return md.FloatNumber(0)
	}
	return md.FloatNumber(f)
}

func isFloat[N number]() bool {
	var n N
	return reflect.ValueOf(n).Kind() == reflect.Float64
}

////////////////
// groups

type groupNumberReader[I numberInstrument, A comparable] struct {
	desc          Description
	makeKeyValues func(A) []md.KeyValue
	instruments   *syncMap[A, I] // from A to numberInstrument
}

func (g *groupNumberReader[I, A]) Descriptions() []Description {
	return []Description{g.desc}
}

// Returns nil if the group is empty.
func (g *groupNumberReader[I, A]) Read([]string) []md.Metric {
	var dps []md.NumberDataPoint
	g.instruments.Range(func(attrs A, ni I) bool {
		dp := ni.read()
		dp.Attributes = g.makeKeyValues(attrs)
		dps = append(dps, dp)
		return true
	})
	if len(dps) == 0 {
		return nil
	}
	return []md.Metric{newNumberMetric(g.desc, dps)}
}

func keyValueMaker[A comparable]() func(A) []md.KeyValue {
	var x A
	v := reflect.ValueOf(x)
	if v.Kind() != reflect.Struct {
		panicf("metrics: attribute type %T is not a struct", x)
	}
	t := v.Type()
	var funcs []func(reflect.Value) md.KeyValue
	vfields := reflect.VisibleFields(t)
	sort.Slice(vfields, func(i, j int) bool { return vfields[i].Name < vfields[j].Name })
	for _, f := range vfields {
		if !f.IsExported() {
			continue
		}
		var cvt func(reflect.Value) any
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			cvt = func(v reflect.Value) any { return v.Int() }
		case reflect.Bool:
			cvt = func(v reflect.Value) any { return v.Bool() }
		case reflect.String:
			cvt = func(v reflect.Value) any { return v.String() }
		default:
			panicf("metrics: invalid type for label struct field %s: %s", f.Name, f.Type)
		}
		name := lowerFirst(f.Name)
		index := f.Index
		funcs = append(funcs, func(v reflect.Value) md.KeyValue {
			return md.KeyValue{
				Key:   name,
				Value: cvt(v.FieldByIndex(index)),
			}
		})
	}
	return func(a A) []md.KeyValue {
		v := reflect.ValueOf(a)
		kvs := make([]md.KeyValue, len(funcs))
		for i, fn := range funcs {
			kvs[i] = fn(v)
		}
		return kvs
	}
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

func panicf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

func errorf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	errorHandler(fmt.Errorf(format, args...))
}
