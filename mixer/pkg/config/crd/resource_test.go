// Copyright 2017 Istio Authors
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

package crd

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"

	cfg "istio.io/mixer/pkg/config/proto"
)

// deepEqualNonNil deeply checks v1 and v2 and returns true they are
// equivalent. Similar to reflect.DeepEqual, but this allows missing
// keys in a map if those values are nil in the other map.
func deepEqualNonNil(v1 interface{}, v2 interface{}) bool {
	if v1 == nil && v2 != nil {
		return false
	}
	if v1 != nil && v2 == nil {
		return false
	}
	switch x1 := v1.(type) {
	case map[string]interface{}:
		x2, ok := v2.(map[string]interface{})
		if !ok {
			return false
		}
		v2Keys := map[string]bool{}
		for v2k, v2v := range x2 {
			if v2v != nil {
				v2Keys[v2k] = true
			}
		}
		for k, xv1 := range x1 {
			xv2, ok := x2[k]
			if !ok && xv1 != nil {
				return false
			}
			if !deepEqualNonNil(xv1, xv2) {
				return false
			}
			delete(v2Keys, k)
		}
		if len(v2Keys) > 0 {
			return false
		}
		return true
	case []interface{}:
		x2, ok := v2.([]interface{})
		if !ok {
			return false
		}
		if len(x1) != len(x2) {
			return false
		}
		for i, xv1 := range x1 {
			if !deepEqualNonNil(xv1, x2[i]) {
				return false
			}
		}
		return true
	default:
		return v1 == v2
	}
}

func TestConvert(t *testing.T) {
	for _, tt := range []struct {
		title    string
		source   map[string]interface{}
		dest     proto.Message
		expected proto.Message
	}{
		{
			"base",
			map[string]interface{}{"name": "foo", "adapter": "a", "params": nil},
			&cfg.Handler{},
			&cfg.Handler{Name: "foo", Adapter: "a"},
		},
		{
			"empty",
			map[string]interface{}{},
			&cfg.Handler{},
			&cfg.Handler{},
		},
	} {
		if err := convert(tt.source, tt.dest); err != nil {
			t.Errorf("Failed to convert %s: %v", tt.title, err)
		}
		if !reflect.DeepEqual(tt.dest, tt.expected) {
			t.Errorf("%s: Got %+v, Want %+v", tt.title, tt.dest, tt.expected)
		}
		back := map[string]interface{}{}
		if err := convertBack(tt.dest, &back); err != nil {
			t.Errorf("Failed to convert back %s: %v", tt.title, err)
		}
		if !deepEqualNonNil(tt.source, back) {
			t.Errorf("%s: Got %+v, Want %+v", tt.title, back, tt.source)
		}
	}
}
