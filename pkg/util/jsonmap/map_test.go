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

package jsonmap

import (
	"reflect"
	"strings"
	"testing"
)

func setup() Map {
	return Map{
		"nested": map[string]interface{}{
			"inner": "innerval",
		},
		"outer": "outerval",
	}
}

func TestMap_Map(t *testing.T) {
	tests := []struct {
		key  string
		want Map
	}{
		{"nested", map[string]interface{}{"inner": "innerval"}},
		{"nested.inner", nil},
		{"outer", nil},
		{"invalid", nil},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := setup()
			p := strings.Split(tt.key, ".")
			for _, k := range p {
				m = m.Map(k)
			}
			if !reflect.DeepEqual(m, tt.want) {
				t.Fatalf("expected:\n %v\n got:\n %v", tt.want, m)
			}
		})
	}
}

func TestMap_Ensure(t *testing.T) {
	tests := []struct {
		key  string
		want Map
	}{
		{"nested", map[string]interface{}{"inner": "innerval"}},
		{"nested.inner", Map{}},
		{"outer", Map{}},
		{"invalid", Map{}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := setup()
			p := strings.Split(tt.key, ".")
			for _, k := range p {
				m = m.Ensure(k)
			}
			if !reflect.DeepEqual(m, tt.want) {
				t.Fatalf("expected:\n %v\n got:\n %v", tt.want, m)
			}
		})
	}
}

func TestMap_String(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"nested", ""},
		{"nested.inner", "innerval"},
		{"outer", "outerval"},
		{"invalid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := setup()
			p := strings.Split(tt.key, ".")
			got := "dontmatchthis"
			for i, k := range p {
				if i == len(p)-1 {
					got = m.String(k)
				} else {
					m = m.Map(k)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected:\n %v\n got:\n %v", tt.want, got)
			}
		})
	}
}
