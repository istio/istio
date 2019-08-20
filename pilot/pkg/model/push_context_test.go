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
	"istio.io/istio/pkg/config/labels"
	"reflect"
	"testing"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestMergeUpdateRequest(t *testing.T) {
	push0 := &PushContext{}
	// trivially different push contexts just for testing
	push1 := &PushContext{ProxyStatus: make(map[string]map[string]ProxyPushStatus)}

	var t0 time.Time
	t1 := t0.Add(time.Minute)

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
			&PushRequest{
				Full:             true,
				Push:             push0,
				Start:            t0,
				TargetNamespaces: map[string]struct{}{"ns1": {}},
			},
			&PushRequest{
				Full:             false,
				Push:             push1,
				Start:            t1,
				TargetNamespaces: map[string]struct{}{"ns2": {}},
			},
			PushRequest{
				Full:             true,
				Push:             push1,
				Start:            t0,
				TargetNamespaces: map[string]struct{}{"ns1": {}, "ns2": {}},
			},
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

func TestEnvoyFilters(t *testing.T) {
	envoyFilters := []*EnvoyFilterWrapper{
		{
			workloadSelector: map[string]string{"app": "v1"},
			Patches:          nil,
		},
	}

	push := &PushContext{
		Env: &Environment{
			Mesh: &meshconfig.MeshConfig{
				RootNamespace: "istio-system",
			},
		},
		envoyFiltersByNamespace: map[string][]*EnvoyFilterWrapper{
			"istio-system": envoyFilters,
			"test-ns":      envoyFilters,
		},
	}

	cases := []struct {
		name     string
		proxy    *Proxy
		expected int
	}{
		{
			name:     "proxy matches two envoyfilters",
			proxy:    &Proxy{ConfigNamespace: "test-ns", WorkloadLabels: labels.Collection{{"app": "v1"}}},
			expected: 2,
		},
		{
			name:     "proxy in root namespace matches an envoyfilter",
			proxy:    &Proxy{ConfigNamespace: "istio-system", WorkloadLabels: labels.Collection{{"app": "v1"}}},
			expected: 1,
		},

		{
			name:     "proxy matches no envoyfilter",
			proxy:    &Proxy{ConfigNamespace: "test-ns", WorkloadLabels: labels.Collection{{"app": "v2"}}},
			expected: 0,
		},

		{
			name:     "proxy matches envoyfilter in root ns",
			proxy:    &Proxy{ConfigNamespace: "test-n2", WorkloadLabels: labels.Collection{{"app": "v1"}}},
			expected: 1,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			filters := push.EnvoyFilters(tt.proxy)
			if len(filters) != tt.expected {
				t.Errorf("Expect %d envoy filters, but got %d", len(filters), tt.expected)
			}
		})
	}

}
