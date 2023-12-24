// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics_test

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"

	"github.com/jba/metrics"
)

func ExampleCounter() {
	c := metrics.NewCounter[int64]("metrics_test/reqs", "total reqs")
	c.Add(7)
	f := func(name string) bool {
		return strings.HasPrefix(name, "metrics_test/") ||
			name == "runtime/gc/heap/allocs"
	}
	h := metrics.NewHandler(nil, f)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", body)
}
