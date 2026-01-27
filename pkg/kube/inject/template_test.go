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

package inject

import (
	"reflect"
	"testing"
)

func TestOmitNil(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{
			name: "no nils",
			in:   map[string]any{"a": 1, "b": "c"},
			want: map[string]any{"a": 1, "b": "c"},
		},
		{
			name: "top level nil",
			in:   map[string]any{"a": nil, "b": "c"},
			want: map[string]any{"b": "c"},
		},
		{
			name: "nested nil",
			in:   map[string]any{"a": map[string]any{"x": nil, "y": 2}, "b": "c"},
			want: map[string]any{"a": map[string]any{"y": 2}, "b": "c"},
		},
		{
			name: "slice with nil",
			in:   []any{1, nil, 3},
			want: []any{1, 3},
		},
		{
			name: "nested slice nil",
			in:   map[string]any{"a": []any{nil, map[string]any{"x": nil}}},
			want: nil,
		},
		{
			name: "all nils map",
			in:   map[string]any{"a": nil, "b": map[string]any{"x": nil}},
			want: nil,
		},
		{
			name: "complex mixed",
			in: map[string]any{
				"global": map[string]any{
					"proxy": map[string]any{
						"resources": map[string]any{
							"limits": map[string]any{
								"cpu":    nil,
								"memory": "500Mi",
							},
						},
					},
				},
			},
			want: map[string]any{
				"global": map[string]any{
					"proxy": map[string]any{
						"resources": map[string]any{
							"limits": map[string]any{
								"memory": "500Mi",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := omitNil(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
