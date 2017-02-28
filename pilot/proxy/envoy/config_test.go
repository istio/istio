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

package envoy

import (
	"io/ioutil"
	"sort"
	"testing"

	"istio.io/manager/test/mock"
)

func TestRoutesByPath(t *testing.T) {
	cases := []struct {
		in       []*HTTPRoute
		expected []*HTTPRoute
	}{

		// Case 2: Prefix before path
		{
			in: []*HTTPRoute{
				{Prefix: "/api"},
				{Path: "/api/v1"},
			},
			expected: []*HTTPRoute{
				{Path: "/api/v1"},
				{Prefix: "/api"},
			},
		},

		// Case 3: Longer prefix before shorter prefix
		{
			in: []*HTTPRoute{
				{Prefix: "/api"},
				{Prefix: "/api/v1"},
			},
			expected: []*HTTPRoute{
				{Prefix: "/api/v1"},
				{Prefix: "/api"},
			},
		},
	}

	// Function to determine if two *Route slices
	// are the same (same Routes, same order)
	sameOrder := func(r1, r2 []*HTTPRoute) bool {
		for i, r := range r1 {
			if r.Path != r2[i].Path || r.Prefix != r2[i].Prefix {
				return false
			}
		}
		return true
	}

	for i, c := range cases {
		sort.Sort(RoutesByPath(c.in))
		if !sameOrder(c.in, c.expected) {
			t.Errorf("Invalid sort order for case %d", i)
		}
	}
}

const (
	envoyData   = "testdata/envoy.json"
	envoyGolden = "testdata/envoy.json.golden"
)

func TestMockConfigGenerate(t *testing.T) {
	ds := mock.Discovery
	r := mock.MakeRegistry()
	config := Generate(
		ds.HostInstances(map[string]bool{mock.HostInstance: true}),
		ds.Services(), r,
		DefaultMeshConfig)
	if config == nil {
		t.Fatalf("Failed to generate config")
	}
	err := config.WriteFile(envoyData)
	if err != nil {
		t.Fatalf(err.Error())
	}

	expected, err := ioutil.ReadFile(envoyGolden)
	if err != nil {
		t.Fatalf(err.Error())
	}
	data, err := ioutil.ReadFile(envoyData)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// TODO: use difflib to obtain detailed diff
	if string(expected) != string(data) {
		t.Errorf("Envoy config master copy changed")
	}
}
