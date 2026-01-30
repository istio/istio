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

package telemetry

import (
	"reflect"
	"testing"

	"istio.io/istio/pkg/config/resource"
)

func TestGetNames(t *testing.T) {
	tests := []struct {
		name     string
		entries  []*resource.Instance
		expected []string
	}{
		{
			name:     "empty entries",
			entries:  []*resource.Instance{},
			expected: []string{},
		},
		{
			name: "single entry",
			entries: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "telemetry-1"),
					},
				},
			},
			expected: []string{"telemetry-1"},
		},
		{
			name: "multiple entries",
			entries: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "telemetry-b"),
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("default", "telemetry-a"),
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("istio-system", "telemetry-c"),
					},
				},
			},
			expected: []string{"telemetry-a", "telemetry-b", "telemetry-c"},
		},
		{
			name: "entries are sorted alphabetically",
			entries: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns", "zeta"),
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns", "alpha"),
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns", "beta"),
					},
				},
			},
			expected: []string{"alpha", "beta", "zeta"},
		},
		{
			name: "entries with same name in different namespaces",
			entries: []*resource.Instance{
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns1", "config"),
					},
				},
				{
					Metadata: resource.Metadata{
						FullName: resource.NewFullName("ns2", "config"),
					},
				},
			},
			expected: []string{"config", "config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getNames(tt.entries)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("getNames() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetNamesNil(t *testing.T) {
	result := getNames(nil)
	if result == nil {
		t.Error("getNames(nil) should return empty slice, not nil")
	}
	if len(result) != 0 {
		t.Errorf("getNames(nil) should return empty slice, got %v", result)
	}
}
