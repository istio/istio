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

package translate

import (
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/util"
)

func TestSetTree(t *testing.T) {
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
			desc:    "insert no path",
			path:    "",
			value:   "val1",
			want:    `val1`,
			wantErr: "path cannot be empty",
		},
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
			err := setTree(root, p, tt.value)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Errorf("TestSetYAML()%s: gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
				return
			}
			if got, want := util.ToYAML(root), tt.want; err == nil && util.YAMLDiff(got, want) != "" {
				t.Errorf("TestSetYAML(%s) got:\n%s\nwant:\n%s\ndiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}
