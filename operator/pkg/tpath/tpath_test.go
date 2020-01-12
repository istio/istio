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

package tpath

import (
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/util"
)

func TestWritePathContext(t *testing.T) {
	rootYAML := `
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - v1
    - v2
    - v3_regex
`
	tests := []struct {
		desc      string
		path      string
		value     interface{}
		want      string
		wantFound bool
		wantErr   string
	}{
		{
			desc:      "ModifyListEntryValue",
			path:      `a.b.[name:n1].value`,
			value:     `v2`,
			wantFound: true,
			want: `
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
		},
		{
			desc:      "ModifyListEntryValueQuoted",
			path:      `a.b.[name:n1].value`,
			value:     `v2`,
			wantFound: true,
			want: `
a:
  b:
  - name: "n1"
    value: v2
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
`,
		},
		{
			desc:      "ModifyListEntry",
			path:      `a.b.[name:n2].list.[v2]`,
			value:     `v3`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v3
    - v3_regex
    name: n2
`,
		},
		{
			desc:      "DeleteListEntry",
			path:      `a.b.[name:n1]`,
			wantFound: true,
			want: `
a:
  b:
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
`,
		},
		{
			desc:      "DeleteListEntryValue",
			path:      `a.b.[name:n2].list.[v2]`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v3_regex
    name: n2
`,
		},
		{
			desc:      "DeleteListEntryValueRegex",
			path:      `a.b.[name:n2].list.[v3]`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v2
    name: n2
`,
		},
		{
			desc:      "AddMapEntry",
			path:      `a.test`,
			value:     `foo`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - v1
    - v2
    - v3_regex
  test: foo
`,
		},
		{
			desc:      "path not found",
			path:      `a.c.[name:n2].list.[v3]`,
			wantFound: false,
			wantErr:   `path not found at element c in path a.c.[name:n2].list.[v3]`,
		},
		{
			desc:      "error key",
			path:      `a.b.[].list`,
			wantFound: false,
			wantErr:   `path a.b.[].list: [] is not a valid value path element`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]interface{})
			if err := yaml.Unmarshal([]byte(rootYAML), &root); err != nil {
				t.Fatal(err)
			}
			pc, gotFound, gotErr := GetPathContext(root, util.PathFromString(tt.path))
			if gotErr, wantErr := errToString(gotErr), tt.wantErr; gotErr != wantErr {
				t.Fatalf("YAMLManifestPatch(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if gotFound != tt.wantFound {
				t.Fatalf("YAMLManifestPatch(%s): gotFound:%v, wantFound:%v", tt.desc, gotFound, tt.wantFound)
			}
			if tt.wantErr != "" || !tt.wantFound {
				return
			}

			err := WritePathContext(pc, tt.value)
			if err != nil {
				t.Fatal(err)
			}

			gotYAML := util.ToYAML(root)
			diff := util.YAMLDiff(gotYAML, tt.want)
			if diff != "" {
				t.Errorf("%s: diff:\n%s\n", tt.desc, diff)
			}
		})
	}
}

func TestWriteNode(t *testing.T) {
	testTreeYAML := `
a:
  b:
    c: val1
    list1:
    - i1: val1
    - i2: val2
    - i3a: key1
      i3b:
        list2:
        - i1: val1
        - i2: val2
        - i3a: key1
          i3b:
            i1: va11
`
	tests := []struct {
		desc     string
		baseYAML string
		path     string
		value    string
		want     string
		wantErr  string
	}{
		{
			desc:  "insert empty",
			path:  "a.b.c",
			value: "val1",
			want: `
a:
  b:
    c: val1
`,
		},
		{
			desc:     "overwrite",
			baseYAML: testTreeYAML,
			path:     "a.b.c",
			value:    "val2",
			want: `
a:
  b:
    c: val2
    list1:
    - i1: val1
    - i2: val2
    - i3a: key1
      i3b:
        list2:
        - i1: val1
        - i2: val2
        - i3a: key1
          i3b:
            i1: va11
`,
		},
		{
			desc:     "partial create",
			baseYAML: testTreeYAML,
			path:     "a.b.d",
			value:    "val3",
			want: `
a:
  b:
    c: val1
    d: val3
    list1:
    - i1: val1
    - i2: val2
    - i3a: key1
      i3b:
        list2:
        - i1: val1
        - i2: val2
        - i3a: key1
          i3b:
            i1: va11
`,
		},
		{
			desc:     "list keys",
			baseYAML: testTreeYAML,
			path:     "a.b.list1.[i3a:key1].i3b.list2.[i3a:key1].i3b.i1",
			value:    "val2",
			want: `
a:
  b:
    c: val1
    list1:
    - i1: val1
    - i2: val2
    - i3a: key1
      i3b:
        list2:
        - i1: val1
        - i2: val2
        - i3a: key1
          i3b:
            i1: val2
`,
		}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]interface{})
			if tt.baseYAML != "" {
				if err := yaml.Unmarshal([]byte(tt.baseYAML), &root); err != nil {
					t.Fatal(err)
				}
			}
			p := util.PathFromString(tt.path)
			err := WriteNode(root, p, tt.value)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Errorf("%s: gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
				return
			}
			if got, want := util.ToYAML(root), tt.want; err == nil && util.YAMLDiff(got, want) != "" {
				t.Errorf("%s: got:\n%s\nwant:\n%s\ndiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
