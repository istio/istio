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

package labels_test

import (
	"testing"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/test/util/assert"
)

func TestInstance(t *testing.T) {
	type result struct {
		subsetOf bool
		selected bool
	}

	cases := []struct {
		left     labels.Instance
		right    labels.Instance
		expected result
	}{
		{
			left:     nil,
			right:    labels.Instance{"app": "a"},
			expected: result{true, false},
		},
		{
			left:     labels.Instance{},
			right:    labels.Instance{"app": "a"},
			expected: result{true, false},
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    nil,
			expected: result{false, false},
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    labels.Instance{"app": "a", "prod": "env"},
			expected: result{true, true},
		},
		{
			left:     labels.Instance{"app": "a", "prod": "env"},
			right:    labels.Instance{"app": "a"},
			expected: result{false, false},
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    labels.Instance{"app": "b", "prod": "env"},
			expected: result{false, false},
		},
		{
			left:     labels.Instance{"foo": ""},
			right:    labels.Instance{"app": "a"},
			expected: result{false, false},
		},
		{
			left:     labels.Instance{"foo": ""},
			right:    labels.Instance{"app": "a", "foo": ""},
			expected: result{true, true},
		},
		{
			left:     labels.Instance{"app": "a", "foo": ""},
			right:    labels.Instance{"foo": ""},
			expected: result{false, false},
		},
	}
	for _, c := range cases {
		var got result
		got.subsetOf = c.left.SubsetOf(c.right)
		got.selected = c.left.Match(c.right)
		if got != c.expected {
			t.Errorf("%v.SubsetOf(%v) got %v, expected %v", c.left, c.right, got, c.expected)
		}
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		input    labels.Instance
		expected string
	}{
		{
			input:    nil,
			expected: "",
		},
		{
			input:    labels.Instance{},
			expected: "",
		},
		{
			input:    labels.Instance{"app": "a"},
			expected: "app=a",
		},
		{
			input:    labels.Instance{"app": "a", "prod": "env"},
			expected: "app=a,prod=env",
		},
		{
			input:    labels.Instance{"foo": ""},
			expected: "foo",
		},
		{
			input:    labels.Instance{"app": "a", "foo": ""},
			expected: "app=a,foo",
		},
	}
	for _, tt := range cases {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.input.String(), tt.expected)
		})
	}
}

func TestInstanceValidate(t *testing.T) {
	cases := []struct {
		name  string
		tags  labels.Instance
		valid bool
	}{
		{
			name:  "empty tags",
			valid: true,
		},
		{
			name: "bad tag",
			tags: labels.Instance{"^": "^"},
		},
		{
			name:  "good tag",
			tags:  labels.Instance{"key": "value"},
			valid: true,
		},
		{
			name:  "good tag - empty value",
			tags:  labels.Instance{"key": ""},
			valid: true,
		},
		{
			name:  "good tag - DNS prefix",
			tags:  labels.Instance{"k8s.io/key": "value"},
			valid: true,
		},
		{
			name:  "good tag - subdomain DNS prefix",
			tags:  labels.Instance{"app.kubernetes.io/name": "value"},
			valid: true,
		},
		{
			name: "bad tag - empty key",
			tags: labels.Instance{"": "value"},
		},
		{
			name: "bad tag key 1",
			tags: labels.Instance{".key": "value"},
		},
		{
			name: "bad tag key 2",
			tags: labels.Instance{"key_": "value"},
		},
		{
			name: "bad tag key 3",
			tags: labels.Instance{"key$": "value"},
		},
		{
			name: "bad tag key - invalid DNS prefix",
			tags: labels.Instance{"istio./key": "value"},
		},
		{
			name: "bad tag value 1",
			tags: labels.Instance{"key": ".value"},
		},
		{
			name: "bad tag value 2",
			tags: labels.Instance{"key": "value_"},
		},
		{
			name: "bad tag value 3",
			tags: labels.Instance{"key": "value$"},
		},
	}
	for _, c := range cases {
		if got := c.tags.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func BenchmarkLabelString(b *testing.B) {
	big := labels.Instance{}
	for i := 0; i < 50; i++ {
		big["topology.kubernetes.io/region"] = "some value"
	}
	small := labels.Instance{
		"app": "foo",
		"baz": "bar",
	}
	cases := []struct {
		name  string
		label labels.Instance
	}{
		{"small", small},
		{"big", big},
	}
	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_ = tt.label.String()
			}
		})
	}
}
