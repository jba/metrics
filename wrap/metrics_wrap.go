// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO: match prefix segment-wise: "foo" matches "foo/" but not "foolish".

package metrics

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	ometric "go.opentelemetry.io/otel/metric"
)

type Number interface {
	~int64 | ~float64
}

type oNumber interface{ int64 | float64 }

////////////////////////////////////////////////////////////////

// A Counter is a cumulative count that is always increasing.
type Counter[N Number] struct {
	c otelCounter[N]
	s attribute.Set
}

// NewCounter creates and registers a Counter.
//
// The corresponding metric has the given name and description, and is
// cumulative and summable. If N is time.Duration, the unit will be
// "ns" (the [UCUM] name for nanonseconds). If N has a method
//
//	Unit() string
//
// then the unit will be the result of calling that method on a zero
// value of N. Otherwise, the unit will be the empty string.
//
// [UCUM]: https://ucum.org/ucum
func NewCounter[N Number](meter ometric.Meter, name, desc string) *Counter[N] {
	return &Counter[N]{c: newOtelCounter[N](meter, name, desc)}
}

// Add adds n to the value of the counter.
// If n is negative, NaN, or infinity, it is not added and
// the error handler (see [SetErrorHandler]) is called with an appropriate error.
func (c *Counter[N]) Add(n N) {
	if n < 0 {
		handleError(&IgnoreValueError{Metric: c.c.Name(), Value: n})
	} else {
		c.c.Add(n, c.s)
	}
}

type otelCounter[N Number] interface {
	Name() string
	Add(N, attribute.Set)
}

func newOtelCounter[N Number](meter ometric.Meter, name, desc string) otelCounter[N] {
	dOpt := ometric.WithDescription(desc)
	uOpt := ometric.WithUnit(unit[N]())
	if isFloat[N]() {
		fc, err := meter.Float64Counter(name, dOpt, uOpt)
		if err != nil {
			handleError(err)
			return nil
		}
		return oFloat64Counter[N]{name: name, c: fc}
	} else {
		ic, err := meter.Int64Counter(name, dOpt, uOpt)
		if err != nil {
			handleError(err)
			return nil
		}
		return oInt64Counter[N]{name: name, c: ic}
	}
}

type oInt64Counter[N Number] struct {
	name string
	c    ometric.Int64Counter
}

func (c oInt64Counter[N]) Name() string { return c.name }

func (c oInt64Counter[N]) Add(n N, s attribute.Set) {
	c.c.Add(context.Background(), int64(n), ometric.WithAttributeSet(s))
}

type oFloat64Counter[N Number] struct {
	name string
	c    ometric.Float64Counter
}

func (c oFloat64Counter[N]) Add(n N, s attribute.Set) {
	f := float64(n)
	if checkFloat(c.name, f) {
		c.c.Add(context.Background(), f, ometric.WithAttributeSet(s))
	}
}

func (c oFloat64Counter[N]) Name() string { return c.name }

func checkFloat(prefix string, f float64) bool {
	if math.IsNaN(f) || math.IsInf(f, +1) || math.IsInf(f, -1) {
		handleError(&IgnoreValueError{Metric: prefix, Value: f})
		return false
	}
	return true
}

////////////////////////////////////////////////////////////////

// A Group is a collection of instruments, each with a different
// value of Attrs. Attrs must be a struct.
type Group[I any, Attrs comparable] struct {
	m                *syncMap[Attrs, I]
	makeAttributeSet func(Attrs) attribute.Set
	new              func(attribute.Set) I
}

func newGroup[I any, A comparable](new func(attribute.Set) I) Group[I, A] {
	return Group[I, A]{
		m:                &syncMap[A, I]{},
		makeAttributeSet: attributeSetMaker[A](),
		new:              new,
	}
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
		s := g.makeAttributeSet(attrs)
		inst, _ = g.m.LoadOrStore(attrs, g.new(s))
	}
	return inst
}

func attributeSetMaker[A comparable]() func(A) attribute.Set {
	var x A
	v := reflect.ValueOf(x)
	if v.Kind() != reflect.Struct {
		panicf("metrics: attribute type %T is not a struct", x)
	}
	t := v.Type()
	var funcs []func(reflect.Value) attribute.KeyValue
	vfields := reflect.VisibleFields(t)
	slices.SortFunc(vfields, func(f1, f2 reflect.StructField) int {
		return cmp.Compare(f1.Name, f2.Name)
	})
	for _, f := range vfields {
		if !f.IsExported() || f.Anonymous {
			continue
		}
		name := lowerFirst(f.Name)
		index := f.Index
		var fn func(v reflect.Value) attribute.KeyValue
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fn = func(v reflect.Value) attribute.KeyValue {
				return attribute.Int64(name, v.FieldByIndex(index).Int())
			}
		case reflect.Bool:
			fn = func(v reflect.Value) attribute.KeyValue {
				return attribute.Bool(name, v.FieldByIndex(index).Bool())
			}
		case reflect.String:
			fn = func(v reflect.Value) attribute.KeyValue {
				return attribute.String(name, v.FieldByIndex(index).String())
			}
		default:
			panicf("metrics: invalid type for label struct field %s: %s", f.Name, f.Type)
		}
		funcs = append(funcs, fn)
	}
	return func(a A) attribute.Set {
		v := reflect.ValueOf(a)
		// TODO: sync.Pool
		kvs := make([]attribute.KeyValue, len(funcs))
		for i, fn := range funcs {
			kvs[i] = fn(v)
		}
		return attribute.NewSet(kvs...)
	}
}

// NewCounterGroup creates a group of counters that differ in the
// values of the type A, which must be a struct.
// See [NewCounter] for details about the corresponding metric.
func NewCounterGroup[N Number, A comparable](meter ometric.Meter, name, desc string) Group[*Counter[N], A] {
	oc := newOtelCounter[N](meter, name, desc)
	return newGroup[*Counter[N], A](func(s attribute.Set) *Counter[N] { return &Counter[N]{oc, s} })
}

////////////////////////////////////////////////////////////////

var (
	mu           sync.Mutex
	errorHandler = func(err error) {
		fmt.Fprintln(os.Stderr, err)
	}
)

// SetErrorHandler sets a function to be called when
// an error happens while creating or recording a metric.
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

type IgnoreValueError struct {
	Metric string
	Value  any
}

func (e *IgnoreValueError) Error() string {
	return fmt.Sprintf("metrics: ignoring bad value for metric %q: %v", e.Metric, e.Value)
}

func handleError(err error) {
	mu.Lock()
	defer mu.Unlock()
	errorHandler(err)
}

func errorf(format string, args ...any) {
	handleError(fmt.Errorf(format, args...))
}

////////////////////////////////////////////////////////////////

func unit[N Number]() string {
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

func isFloat[N Number]() bool {
	var n N
	return reflect.ValueOf(n).Kind() == reflect.Float64
}
func panicf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}
