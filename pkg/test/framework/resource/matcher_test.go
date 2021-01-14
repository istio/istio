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

package resource

import "testing"

func TestMatcher(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		matches   []string
		nomatches []string
	}{
		{
			name:      "empty",
			input:     "",
			matches:   []string{},
			nomatches: []string{"", "foo", "foo/bar", ".*"},
		},
		{
			name:      "single",
			input:     "Foo",
			matches:   []string{"TestFoo", "TestMyFooBar", "TestFoo/TestBar"},
			nomatches: []string{"baz", "baz/foo", "TestBar/TestFoo"},
		},
		{
			name:      "double",
			input:     "Foo/Bar",
			matches:   []string{"TestFoo/TestBar", "TestFoo/TestBar/TestBaz"},
			nomatches: []string{"TestFoo", "TestBar", "TestMyFooBar"},
		},
		{
			name:    "space",
			input:   "TestFoo/with space",
			matches: []string{"TestFoo/with_space"},
		},
		{
			name:      "regex",
			input:     "Foo/.*/Baz",
			matches:   []string{"TestFoo/something/TestBaz"},
			nomatches: []string{"TestFoo", "TestFoo/TestBaz", "TestFoo/something/TestBar"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewMatcher(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range tt.matches {
				got := matcher.MatchTest(m)
				if !got {
					t.Errorf("expected match for %q", m)
				}
			}
			for _, m := range tt.nomatches {
				got := matcher.MatchTest(m)
				if got {
					t.Errorf("expected no match for %q", m)
				}
			}
		})
	}
}
