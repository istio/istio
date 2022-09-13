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

package tpath

import (
	"errors"
	"testing"
)

func TestAddSpecRoot(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect string
		err    error
	}{
		{
			desc: "empty",
			in:   ``,
			expect: `spec: {}
`,
			err: nil,
		},
		{
			desc: "add-root",
			in: `
a: va
b: foo`,
			expect: `spec:
  a: va
  b: foo
`,
			err: nil,
		},
		{
			desc:   "err",
			in:     `i can't be yaml, can I?`,
			expect: ``,
			err:    errors.New(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, err := AddSpecRoot(tt.in); got != tt.expect ||
				((err != nil && tt.err == nil) || (err == nil && tt.err != nil)) {
				t.Errorf("%s AddSpecRoot(%s) => %s, want %s", tt.desc, tt.in, got, tt.expect)
			}
		})
	}
}

func TestGetConfigSubtree(t *testing.T) {
	tests := []struct {
		desc     string
		manifest string
		path     string
		expect   string
		err      bool
	}{
		{
			desc:     "empty",
			manifest: ``,
			path:     ``,
			expect: `{}
`,
			err: false,
		},
		{
			desc: "subtree",
			manifest: `
a:
  b:
  - name: n1
    value: v2
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
`,
			path: `a`,
			expect: `b:
- name: n1
  value: v2
- list:
  - v1
  - v2
  - v3_regex
  name: n2
`,
			err: false,
		},
		{
			desc:     "err",
			manifest: "not-yaml",
			path:     "not-subnode",
			expect:   ``,
			err:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, err := GetConfigSubtree(tt.manifest, tt.path); got != tt.expect || (err == nil) == tt.err {
				t.Errorf("%s GetConfigSubtree(%s, %s) => %s, want %s", tt.desc, tt.manifest, tt.path, got, tt.expect)
			}
		})
	}
}
