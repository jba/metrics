// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metricsdata

import (
	"encoding/json"
	"fmt"
	"testing"
)

type (
	S struct {
		Key string
		Value
	}

	Value struct {
		AsInt    int64   `json:"asInt,omitempty"`
		AsDouble float64 `json:"asDouble,omitempty"`
	}
)

func TestJSON(t *testing.T) {
	s := S{Key: "k", Value: Value{AsInt: 3}}
	out, err := json.MarshalIndent(s, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%s\n", out)
}
