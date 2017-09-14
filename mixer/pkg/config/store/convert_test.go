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

package store

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"

	cfg "istio.io/mixer/pkg/config/proto"
)

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
		if err := convert(Key{}, tt.source, tt.dest); err != nil {
			t.Errorf("Failed to convert %s: %v", tt.title, err)
		}
		if !reflect.DeepEqual(tt.dest, tt.expected) {
			t.Errorf("%s: Got %+v, Want %+v", tt.title, tt.dest, tt.expected)
		}
	}
}

func TestConvertFail(t *testing.T) {
	h := &cfg.Handler{}
	if err := convert(Key{}, map[string]interface{}{"foo": 1}, h); err == nil {
		t.Errorf("Got nil, Want error")
	}
	if err := convert(Key{}, map[string]interface{}{"foo": map[interface{}]int{nil: 0}}, h); err == nil {
		t.Errorf("Got nil, Want error")
	}
}

func TestCloneWithKind(t *testing.T) {
	for _, c := range []struct {
		kind  string
		kinds map[string]proto.Message
		ok    bool
	}{
		{
			"Handler",
			map[string]proto.Message{
				"Handler": &cfg.Handler{Name: "default", Adapter: "noop"},
				"Action":  &cfg.Action{},
			},
			true,
		},
		{
			"Action",
			map[string]proto.Message{
				"Handler": &cfg.Handler{Name: "default", Adapter: "noop"},
				"Action":  &cfg.Action{},
			},
			true,
		},
		{
			"Unknown",
			map[string]proto.Message{
				"Handler": &cfg.Handler{Name: "default", Adapter: "noop"},
				"Action":  &cfg.Action{},
			},
			false,
		},
		{
			"Empty",
			map[string]proto.Message{},
			false,
		},
	} {
		t.Run(c.kind, func(tt *testing.T) {
			cloned, err := cloneMessage(c.kind, c.kinds)
			if !c.ok {
				if err == nil {
					tt.Errorf("Got nil, Want failure")
				}
				return
			}
			if err != nil {
				tt.Fatalf("Got %v, Want nil", err)
			}
			if cloned == c.kinds[c.kind] {
				tt.Fatalf("Got a pointer, Want a copy")
			}
			if !reflect.DeepEqual(cloned, c.kinds[c.kind]) {
				tt.Errorf("Got %v, Want %v", cloned, c.kinds[c.kind])
			}
		})
	}
}
