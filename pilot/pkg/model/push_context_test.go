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
		left   *PushRequest
		right  *PushRequest
		merged PushRequest
	}{
		{
			"left nil",
			nil,
			&PushRequest{Full: true},
			PushRequest{Full: true},
		},
		{
			"right nil",
			&PushRequest{Full: true},
			nil,
			PushRequest{Full: true},
		},
		{
			"simple merge",
			&PushRequest{Full: true, TargetNamespaces: map[string]struct{}{"ns1": {}}},
			&PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns2": {}}},
			PushRequest{Full: true, TargetNamespaces: map[string]struct{}{"ns1": {}, "ns2": {}}},
		},
		{
			"incremental target namespace merge",
			&PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns1": {}}},
			&PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns2": {}}},
			PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns1": {}, "ns2": {}}},
		},
		{
			"incremental eds merge",
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}, "svc-2": {}}},
		},
		{
			"skip eds merge: left full",
			&PushRequest{Full: true},
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: true},
		},
		{
			"skip eds merge: right full",
			&PushRequest{Full: false, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: true},
			PushRequest{Full: true},
		},
		{
			"incremental merge",
			&PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns1": {}}, EdsUpdates: map[string]struct{}{"svc-1": {}}},
			&PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns2": {}}, EdsUpdates: map[string]struct{}{"svc-2": {}}},
			PushRequest{Full: false, TargetNamespaces: map[string]struct{}{"ns1": {}, "ns2": {}}, EdsUpdates: map[string]struct{}{"svc-1": {}, "svc-2": {}}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.Merge(tt.right)
			if !reflect.DeepEqual(&tt.merged, got) {
				t.Fatalf("expected %v, got %v", tt.merged, got)
			}
		})
	}
}
