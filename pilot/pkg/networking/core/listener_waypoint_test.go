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

package core

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
)

func TestXFCCIncludeClientIdentityEnabled(t *testing.T) {
	cases := []struct {
		name     string
		node     *model.Proxy
		expected bool
	}{
		{
			name:     "nil proxy",
			node:     nil,
			expected: false,
		},
		{
			name:     "no metadata",
			node:     &model.Proxy{},
			expected: false,
		},
		{
			name: "no annotations",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			expected: false,
		},
		{
			name: "annotation unset",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Annotations: map[string]string{"other": "value"},
				},
			},
			expected: false,
		},
		{
			name: "annotation false",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Annotations: map[string]string{xfccClientIdentityAnnotation: "false"},
				},
			},
			expected: false,
		},
		{
			name: "annotation true",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Annotations: map[string]string{xfccClientIdentityAnnotation: "true"},
				},
			},
			expected: true,
		},
		{
			name: "annotation TRUE (case-insensitive)",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Annotations: map[string]string{xfccClientIdentityAnnotation: "TRUE"},
				},
			},
			expected: true,
		},
		{
			name: "annotation 1 is not accepted as true",
			node: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Annotations: map[string]string{xfccClientIdentityAnnotation: "1"},
				},
			},
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := xfccIncludeClientIdentityEnabled(tc.node); got != tc.expected {
				t.Fatalf("xfccIncludeClientIdentityEnabled(%v) = %v, want %v", tc.node, got, tc.expected)
			}
		})
	}
}

func TestWaypointXFCCClientIdentityFilterConfig(t *testing.T) {
	f := xdsfilters.WaypointXFCCClientIdentityFilter
	if f == nil {
		t.Fatal("WaypointXFCCClientIdentityFilter is nil")
	}
	if f.Name != "istio.waypoint.xfcc_client_identity" {
		t.Fatalf("unexpected filter name: %q", f.Name)
	}
	if f.GetTypedConfig() == nil {
		t.Fatal("filter missing typed config")
	}
}
