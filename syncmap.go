// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import "sync"

// Type-safe wrapper around a sync.Map

type syncMap[K comparable, V any] sync.Map

func (m *syncMap[K, V]) Load(k K) (v V, found bool) {
	a, found := (*sync.Map)(m).Load(k)
	if found {
		return a.(V), true
	}
	return v, false
}

func (m *syncMap[K, V]) LoadOrStore(k K, v V) (V, bool) {
	v2, ok := (*sync.Map)(m).LoadOrStore(k, v)
	return v2.(V), ok
}

func (m *syncMap[K, V]) Range(f func(K, V) bool) {
	(*sync.Map)(m).Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
