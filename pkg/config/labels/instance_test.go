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
)

func TestInstance(t *testing.T) {
	cases := []struct {
		left     labels.Instance
		right    labels.Instance
		expected bool
	}{
		{
			left:     nil,
			right:    labels.Instance{"app": "a"},
			expected: true,
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    nil,
			expected: false,
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    labels.Instance{"app": "a", "prod": "env"},
			expected: true,
		},
		{
			left:     labels.Instance{"app": "a", "prod": "env"},
			right:    labels.Instance{"app": "a"},
			expected: false,
		},
		{
			left:     labels.Instance{"app": "a"},
			right:    labels.Instance{"app": "b", "prod": "env"},
			expected: false,
		},
		{
			left:     labels.Instance{"foo": ""},
			right:    labels.Instance{"app": "a"},
			expected: false,
		},
		{
			left:     labels.Instance{"foo": ""},
			right:    labels.Instance{"app": "a", "foo": ""},
			expected: true,
		},
		{
			left:     labels.Instance{"app": "a", "foo": ""},
			right:    labels.Instance{"foo": ""},
			expected: false,
		},
	}
	for _, c := range cases {
		got := c.left.SubsetOf(c.right)
		if got != c.expected {
			t.Errorf("%v.SubsetOf(%v) got %v, expected %v", c.left, c.right, got, c.expected)
		}
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
