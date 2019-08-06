// Copyright 2019 Istio Authors
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

package model

import (
	"reflect"
	"testing"
)

func TestMergeUpdateRequest(t *testing.T) {
	cases := []struct {
		name   string
		left   *UpdateRequest
		right  *UpdateRequest
		merged UpdateRequest
	}{
		{
			"left nil",
			nil,
			&UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   nil,
			},
			UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   nil,
			},
		},
		{
			"right nil",
			&UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   nil,
			},
			nil,
			UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   nil,
			},
		},
		{
			"simple merge",
			&UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   map[string]struct{}{"ns1": {}},
			},
			&UpdateRequest{
				Full:               false,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   map[string]struct{}{"ns2": {}},
			},
			UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   map[string]struct{}{"ns1": {}, "ns2": {}},
			},
		},
		{
			"incremental merge",
			&UpdateRequest{
				Full:               false,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   map[string]struct{}{"ns1": {}},
			},
			&UpdateRequest{
				Full:               false,
				ConfigTypesUpdated: map[string]struct{}{ServiceEntry.Type: {}},
				TargetNamespaces:   map[string]struct{}{"ns2": {}},
			},
			UpdateRequest{
				Full:               false,
				ConfigTypesUpdated: nil,
				TargetNamespaces:   map[string]struct{}{"ns1": {}, "ns2": {}},
			},
		},
		{
			"incremental merge with config updates",
			&UpdateRequest{Full:true,
				TargetNamespaces: map[string]struct{}{"ns1": {}},
				ConfigTypesUpdated: map[string]struct{}{ServiceEntry.Type: {}},
			},
			&UpdateRequest{
				Full:               true,
				ConfigTypesUpdated: map[string]struct{}{VirtualService.Type: {}},
				TargetNamespaces: map[string]struct{}{"ns1": {}},
			},
			UpdateRequest{Full:true, TargetNamespaces: map[string]struct{}{"ns1": {}},
				ConfigTypesUpdated: map[string]struct{}{ServiceEntry.Type: {}, VirtualService.Type: {}}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.Merge(tt.right)
			if !reflect.DeepEqual(tt.merged, got) {
				t.Fatalf("expected %v, got %v", tt.merged, got)
			}
		})
	}
}
