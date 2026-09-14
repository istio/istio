// Copyright Istio Authors
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

package features

import (
	"testing"

	"istio.io/istio/pkg/slices"
)

func TestParseProxyConvergenceTimeBuckets(t *testing.T) {
	defaults := []float64{.1, .5, 1, 3, 5, 10, 20, 30}
	cases := []struct {
		name  string
		value string
		want  []float64
	}{
		{"empty", "", defaults},
		{"whitespace only", "  \t", defaults},
		{"custom", "0.01,0.25,1,60,120", []float64{.01, .25, 1, 60, 120}},
		{"whitespace", " 0.1, 1 , 60 ", []float64{.1, 1, 60}},
		{"single", "5", []float64{5}},
		{"zero", "0,0.1,1", []float64{0, .1, 1}},
		{"malformed", "0.1,nope,1", defaults},
		{"empty entry", "0.1,,1", defaults},
		{"leading comma", ",0.1,1", defaults},
		{"trailing comma", "0.1,1,", defaults},
		{"duplicate", "0.1,1,1", defaults},
		{"descending", "1,0.1", defaults},
		{"negative", "-1,0,1", defaults},
		{"nan", "0.1,NaN", defaults},
		{"infinity", "0.1,+Inf", defaults},
		{"negative infinity", "-Inf,1", defaults},
		{"overflow", "0.1,1e999", defaults},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseProxyConvergenceTimeBuckets(tt.value); !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
