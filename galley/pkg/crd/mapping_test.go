//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package crd

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNewMapping_Errors(t *testing.T) {
	tests := []struct {
		input map[schema.GroupVersion]schema.GroupVersion
		err   string
	}{
		{
			input: map[schema.GroupVersion]schema.GroupVersion{
				{
					Group:   "g1",
					Version: "v1",
				}: {
					Group:   "g1",
					Version: "v2",
				},
			},
			err: `cycling mapping not allowed: g1`,
		},
		{
			input: map[schema.GroupVersion]schema.GroupVersion{
				{
					Group:   "g1",
					Version: "v1",
				}: {
					Group:   "g2",
					Version: "v2",
				},
				{
					Group:   "g1",
					Version: "v2",
				}: {
					Group:   "g3",
					Version: "v3",
				},
			},
			err: `mapping already exists: g1`,
		},
		{
			input: map[schema.GroupVersion]schema.GroupVersion{
				{
					Group:   "g1",
					Version: "v1",
				}: {
					Group:   "g2",
					Version: "v2",
				},
				{
					Group:   "g3",
					Version: "v3",
				}: {
					Group:   "g2",
					Version: "v1",
				},
			},
			err: `reverse mapping is not unique: g2`,
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			_, err := NewMapping(tst.input)
			if err == nil {
				tt.Fatalf("error not found: %v", tst.err)
			}

			if strings.TrimSpace(tst.err) != strings.TrimSpace(err.Error()) {
				tt.Fatalf("Unexpected error: got:\n%v\n, wanted:\n%v\n", err, tst.err)
			}
		})
	}
}

func TestMapping_GetGroupVersion(t *testing.T) {
	m, _ := NewMapping(map[schema.GroupVersion]schema.GroupVersion{
		{
			Group:   "g1",
			Version: "v1",
		}: {
			Group:   "g2",
			Version: "v2",
		},
	})

	tests := []struct {
		input       string
		source      schema.GroupVersion
		destination schema.GroupVersion
		found       bool
	}{
		{
			input: "zoo",
			found: false,
		},
		{
			input:       "g1",
			source:      schema.GroupVersion{Group: "g1", Version: "v1"},
			destination: schema.GroupVersion{Group: "g2", Version: "v2"},
			found:       true,
		},
		{
			input:       "g2",
			source:      schema.GroupVersion{Group: "g1", Version: "v1"},
			destination: schema.GroupVersion{Group: "g2", Version: "v2"},
			found:       true,
		},
	}

	for _, tst := range tests {
		t.Run(tst.input, func(tt *testing.T) {
			as, ad, af := m.GetGroupVersion(tst.input)
			if af != tst.found {
				tt.Fatalf("Unexpected 'found': got:'%v', wanted:'%v'", af, tst.found)
			}
			if as != tst.source {
				tt.Fatalf("Unexpected 'source': got:'%v', wanted:'%v'", as, tst.source)
			}
			if ad != tst.destination {
				tt.Fatalf("Unexpected 'destination': got:'%v', wanted:'%v'", ad, tst.destination)
			}
		})
	}
}

func TestMapping_GetVersion(t *testing.T) {
	m, _ := NewMapping(map[schema.GroupVersion]schema.GroupVersion{
		{
			Group:   "g1",
			Version: "v1",
		}: {
			Group:   "g2",
			Version: "v2",
		},
	})

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "g1",
			expected: "v1",
		},
		{
			input:    "g2",
			expected: "v2",
		},
		{
			input:    "g3",
			expected: "",
		},
	}

	for _, tst := range tests {
		t.Run(tst.input, func(tt *testing.T) {
			actual := m.GetVersion(tst.input)
			if actual != tst.expected {
				tt.Fatalf("Unexpected result: got:'%v', wanted:'%v'", actual, tst.expected)
			}
		})
	}
}

func TestMapping_String(t *testing.T) {

	tests := []struct {
		input    map[schema.GroupVersion]schema.GroupVersion
		expected string
	}{
		{
			input: map[schema.GroupVersion]schema.GroupVersion{},
			expected: `
--- Mapping ---
---------------`,
		},
		{
			input: map[schema.GroupVersion]schema.GroupVersion{
				{
					Group:   "g1",
					Version: "v1",
				}: {
					Group:   "g2",
					Version: "v2",
				},
			},
			expected: `
--- Mapping ---
g1/v1 => g2/v2
---------------
`,
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			m, err := NewMapping(tst.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actual := m.String()
			if strings.TrimSpace(actual) != strings.TrimSpace(tst.expected) {
				tt.Fatalf("Unexpected result: got:\n%v\n, wanted:\n%v\n", actual, tst.expected)
			}
		})
	}
}
