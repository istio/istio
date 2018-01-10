// Copyright 2017 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package descriptor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestValidateLabels(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]dpb.ValueType
		err  string
	}{
		{"valid", map[string]dpb.ValueType{"one": dpb.STRING, "two": dpb.INT64}, ""},
		{"invalid", map[string]dpb.ValueType{"one": dpb.STRING, "two": dpb.VALUE_TYPE_UNSPECIFIED}, "labels[two]"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateLabels(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateLabels() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateDescriptorName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		err  string
	}{
		{"valid", "somename", ""},
		{"invalid", "$invalid", ""},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateDescriptorName(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateDescriptorName() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestAlmostEq(t *testing.T) {
	// Move to the nth next representable f64 value
	nthNextFloat := func(a float64, n uint64) float64 {
		return math.Float64frombits(math.Float64bits(a) + n)
	}

	tests := []struct {
		a     float64
		b     float64
		steps int64
		out   bool
	}{
		{1, 1, 0, true},
		{+0, -0, 0, true},
		{float64(1) / 3, float64(1) / 3.000001, 10, false},
		{1, 2, 1000, false},
		{123.0, nthNextFloat(123.0, 2), 1, false},
		{123.0, nthNextFloat(123.0, 2), 2, true},
		{123.0, nthNextFloat(123.0, 2), 3, true},
		{math.Inf(1), math.Inf(1), 0, true},
		{math.Inf(-1), math.Inf(-1), 0, true},
		// the following are not eq no matter how many steps.
		{math.Inf(1), math.Inf(-1), math.MaxInt64, false},
		{math.Inf(-1), math.Inf(1), math.MaxInt64, false},
		{math.NaN(), math.NaN(), math.MaxInt64, false},
		{0, math.NaN(), math.MaxInt64, false},
		{math.NaN(), 0, math.MaxInt64, false},
	}

	for idx, tt := range tests {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			if actual := almostEq(tt.a, tt.b, tt.steps); actual != tt.out {
				t.Fatalf("almostEq(%f, %f, %d) = %t, wanted %t", tt.a, tt.b, tt.steps, actual, tt.out)
			}
		})
	}
}
