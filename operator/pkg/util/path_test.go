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

package util

import (
	"testing"
)

func TestSplitEscaped(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want []string
	}{
		{
			desc: "empty",
			in:   "",
			want: []string{},
		},
		{
			desc: "no match",
			in:   "foo",
			want: []string{"foo"},
		},
		{
			desc: "first",
			in:   ":foo",
			want: []string{"", "foo"},
		},
		{
			desc: "last",
			in:   "foo:",
			want: []string{"foo", ""},
		},
		{
			desc: "multiple",
			in:   "foo:bar:baz",
			want: []string{"foo", "bar", "baz"},
		},
		{
			desc: "multiple with escapes",
			in:   `foo\:bar:baz\:qux`,
			want: []string{`foo\:bar`, `baz\:qux`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := splitEscaped(tt.in, kvSeparatorRune), tt.want; !stringSlicesEqual(got, want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, aa := range a {
		if aa != b[i] {
			return false
		}
	}
	return true
}
