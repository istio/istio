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

package values

import (
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestSetPath(t *testing.T) {
	fromJSON := func(s string) Map {
		m, err := fromJSON[Map]([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name   string
		base   Map
		inPath string
		inData any
		out    string
	}{
		{
			name:   "trivial",
			inPath: "spec",
			inData: 1,
			out:    `{"spec":1}`,
		},
		{
			name:   "simple create",
			inPath: "spec.bar",
			inData: 1,
			out:    `{"spec":{"bar":1}}`,
		},
		{
			name:   "simple merge",
			inPath: "spec.bar",
			base:   Map{"spec": Map{"foo": "baz"}},
			inData: 1,
			out:    `{"spec":{"bar":1,"foo":"baz"}}`,
		},
		{
			name:   "array",
			inPath: "top.[0]",
			inData: 1,
			out:    `{"top":[1]}`,
		},
		{
			name:   "array flattened",
			inPath: "top[0]",
			inData: 1,
			out:    `{"top":[1]}`,
		},
		{
			name:   "array and values",
			inPath: "top.[0].bar",
			inData: 1,
			out:    `{"top":[{"bar":1}]}`,
		},
		{
			name:   "array and values merge",
			inPath: "top.[0].bar",
			base:   fromJSON(`{"top":[{"baz":2}]}`),
			inData: 1,
			out:    `{"top":[{"bar":1,"baz":2}]}`,
		},
		{
			name:   "kv set",
			inPath: "env.[name:POD_NAME].value",
			base:   fromJSON(`{"env":[{"name":"POD_NAME"}]}`),
			inData: 1,
			out:    `{"env":[{"name":"POD_NAME","value":1}]}`,
		},
		{
			name:   "escape kv",
			inPath: "env.[name:foo\\.bar].value",
			base:   fromJSON(`{"env":[{"name":"foo.bar"}]}`),
			inData: "hi",
			out:    `{"env":[{"name":"foo.bar","value":"hi"}]}`,
		},
		{
			name:   "set kv",
			inPath: "spec.ports.[name:https-dns].port",
			base:   fromJSON(`{"spec":{"ports":[{"name":"https-dns"}]}}`),
			inData: 11111,
			out:    `{"spec":{"ports":[{"name":"https-dns","port":11111}]}}`,
		},
		{
			name:   "set unmatched kv",
			inPath: "spec.ports.[name:https-dns].port",
			inData: 11111,
			out:    ``,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := Map{}
			if tt.base != nil {
				m = tt.base
			}
			err := m.SetPath(tt.inPath, tt.inData)
			if tt.out != "" {
				assert.NoError(t, err)
				assert.Equal(t, tt.out, m.JSON())
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestGetPath(t *testing.T) {
	fromJSON := func(s string) Map {
		m, err := fromJSON[Map]([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name string
		base Map
		path string
		out  any
	}{
		{
			name: "trivial",
			base: fromJSON(`{"spec":1}`),
			path: "spec",
			out:  float64(1),
		},
		{
			name: "nested",
			path: "spec.bar",
			base: fromJSON(`{"spec":{"bar":1}}`),
			out:  float64(1),
		},
		{
			name: "map",
			path: "spec",
			base: fromJSON(`{"spec":{"bar":1}}`),
			out:  map[string]any{"bar": float64(1)},
		},
		{
			name: "array",
			path: "top.[0]",
			base: fromJSON(`{"top":[1]}`),
			out:  float64(1),
		},
		{
			name: "array out of bounds",
			path: "top.[9]",
			base: fromJSON(`{"top":[1]}`),
			out:  nil,
		},
		{
			name: "array and values",
			path: "top.[0].bar",
			base: fromJSON(`{"top":[{"bar":1}]}`),
			out:  float64(1),
		},
		{
			name: "kv",
			path: "env.[name:POD_NAME].value",
			base: fromJSON(`{"env":[{"name":"POD_NAME","value":1}]}`),
			out:  float64(1),
		},
		{
			name: "escape kv",
			path: "env.[name:foo\\.bar].value",
			base: fromJSON(`{"env":[{"name":"foo.bar","value":"hi"}]}`),
			out:  "hi",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := tt.base.GetPath(tt.path)
			assert.Equal(t, tt.out, v)
		})
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want any
	}{
		{
			desc: "empty",
			in:   "",
			want: "",
		},
		{
			desc: "true",
			in:   "true",
			want: true,
		},
		{
			desc: "false",
			in:   "false",
			want: false,
		},
		{
			desc: "numeric-one",
			in:   "1",
			want: 1,
		},
		{
			desc: "numeric-zero",
			in:   "0",
			want: 0,
		},
		{
			desc: "numeric-large",
			in:   "12345678",
			want: 12345678,
		},
		{
			desc: "numeric-negative",
			in:   "-12345678",
			want: -12345678,
		},
		{
			desc: "float",
			in:   "1.23456",
			want: 1.23456,
		},
		{
			desc: "float-zero",
			in:   "0.00",
			want: 0.00,
		},
		{
			desc: "float-negative",
			in:   "-6.54321",
			want: -6.54321,
		},
		{
			desc: "string",
			in:   "foobar",
			want: "foobar",
		},
		{
			desc: "string-number-prefix",
			in:   "123foobar",
			want: "123foobar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := parseValue(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestMergeFrom(t *testing.T) {
	fromJSON := func(s string) Map {
		m, err := fromJSON[Map]([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name     string
		base     Map
		other    Map
		expected Map
	}{
		{
			name:     "empty base and other",
			base:     fromJSON(`{}`),
			other:    fromJSON(`{}`),
			expected: fromJSON(`{}`),
		},
		{
			name:     "simple key overwrite",
			base:     fromJSON(`{"key1": "value1"}`),
			other:    fromJSON(`{"key1": "newValue"}`),
			expected: fromJSON(`{"key1": "newValue"}`),
		},
		{
			name:     "simple bool overwrite",
			base:     fromJSON(`{"key1": false}`),
			other:    fromJSON(`{"key1": true}`),
			expected: fromJSON(`{"key1": true}`),
		},
		{
			name:     "new key addition",
			base:     fromJSON(`{"key1": "value1"}`),
			other:    fromJSON(`{"key2": "value2"}`),
			expected: fromJSON(`{"key1": "value1", "key2": "value2"}`),
		},
		{
			name:     "nested map merge",
			base:     fromJSON(`{"nested": {"key1": "value1"}}`),
			other:    fromJSON(`{"nested": {"key2": "value2"}}`),
			expected: fromJSON(`{"nested": {"key1": "value1", "key2": "value2"}}`),
		},
		{
			name:     "nested map overwrite",
			base:     fromJSON(`{"nested": {"key1": "value1"}}`),
			other:    fromJSON(`{"nested": {"key1": "newValue"}}`),
			expected: fromJSON(`{"nested": {"key1": "newValue"}}`),
		},
		{
			name:     "nested map bool overwrite",
			base:     fromJSON(`{"nested": {"key1": false}}`),
			other:    fromJSON(`{"nested": {"key1": true}}`),
			expected: fromJSON(`{"nested": {"key1": true}}`),
		},
		{
			name:     "overwrite primitive with map",
			base:     fromJSON(`{"key": "value"}`),
			other:    fromJSON(`{"key": {"nestedKey": "nestedValue"}}`),
			expected: fromJSON(`{"key": {"nestedKey": "nestedValue"}}`),
		},
		{
			name:     "overwrite map with primitive",
			base:     fromJSON(`{"key": {"nestedKey": "nestedValue"}}`),
			other:    fromJSON(`{"key": "primitiveValue"}`),
			expected: fromJSON(`{"key": "primitiveValue"}`),
		},
		{
			name:     "deeply nested merge",
			base:     fromJSON(`{"a": {"b": {"c": "value1"}}}`),
			other:    fromJSON(`{"a": {"b": {"d": "value2"}}}`),
			expected: fromJSON(`{"a": {"b": {"c": "value1", "d": "value2"}}}`),
		},
		{
			name:     "list replacement",
			base:     fromJSON(`{"key": [1, 2, 3]}`),
			other:    fromJSON(`{"key": [4, 5]}`),
			expected: fromJSON(`{"key": [4, 5]}`), // Lists are replaced, not merged.
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.base.MergeFrom(tt.other)
			assert.Equal(t, tt.expected, tt.base)
		})
	}
}

func TestMergeFromDoesNotModifyOther(t *testing.T) {
	var (
		base = Map{
			"a": Map{
				"b": Map{
					"c": false,
				},
			},
		}
		other = Map{
			"a": Map{
				"b": Map{
					"c": true,
				},
			},
		}
	)

	// Clone `other` before merging
	originalOther := other.DeepClone()

	// Perform merge
	base.MergeFrom(other)

	// Update `c` in `base`
	base.SetPath("a.b.c", false)

	// Ensure `other` remains unchanged
	assert.Equal(t, originalOther, other.DeepClone() /* DeepClone() changes the type to map[string]any */)
}
