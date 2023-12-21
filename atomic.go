// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"math"
	"reflect"
	"sync/atomic"
)

type number interface {
	~int64 | ~float64
}

type atomicNumber[T number] interface {
	Store(T)
	Add(T)
	Load() T
}

type atomicInt64[T number] atomic.Int64

func newAtomicNumber[T number]() atomicNumber[T] {
	var z T
	v := reflect.ValueOf(z)
	switch v.Kind() {
	case reflect.Int64:
		return &atomicInt64[T]{}
	case reflect.Float64:
		return &atomicFloat64[T]{}
	default:
		panic("bad atomic number type")
	}
}

func (a *atomicInt64[T]) Store(x T) {
	(*atomic.Int64)(a).Store(int64(x))
}

func (a *atomicInt64[T]) Add(x T) {
	(*atomic.Int64)(a).Add(int64(x))
}

func (a *atomicInt64[T]) Load() T {
	return T((*atomic.Int64)(a).Load())
}

type atomicFloat64[T number] atomic.Uint64

func (a *atomicFloat64[T]) Store(x T) {
	(*atomic.Uint64)(a).Store(toUint64(x))
}

func (a *atomicFloat64[T]) Load() T {
	return fromUint64[T]((*atomic.Uint64)(a).Load())
}

func (a *atomicFloat64[T]) Add(x T) {
	// copied from expvar.Float.Add.
	for {
		cur := (*atomic.Uint64)(a).Load()
		nxt := toUint64(fromUint64[T](cur) + x)
		if (*atomic.Uint64)(a).CompareAndSwap(cur, nxt) {
			return
		}
	}
}

func toUint64[T number](x T) uint64 {
	return math.Float64bits(float64(x))
}

func fromUint64[T number](u uint64) T {
	return T(math.Float64frombits(u))
}
