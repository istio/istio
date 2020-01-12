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

package name

import (
	"reflect"
	"testing"

	"istio.io/operator/pkg/tpath"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/util"
)

func TestGetFromTreePath(t *testing.T) {
	type args struct {
		inputTree string
		path      util.Path
	}

	tests := []struct {
		name    string
		args    args
		want    string
		found   bool
		wantErr bool
	}{
		{
			name: "found string node",
			args: args{
				inputTree: `
k1: v1
`,
				path: util.Path{"k1"},
			},
			want: `
v1
`,
			found:   true,
			wantErr: false,
		},
		{
			name: "found tree node",
			args: args{
				inputTree: `
k1:
 k2: v2
`,
				path: util.Path{"k1"},
			},
			want: `
k2: v2
`,
			found:   true,
			wantErr: false,
		},
		{
			name: "path is longer than tree depth, string node",
			args: args{
				inputTree: `
k1: v1
`,
				path: util.Path{"k1", "k2"},
			},
			want:    "",
			found:   false,
			wantErr: false,
		},
		{
			name: "path is longer than tree depth, array node",
			args: args{
				inputTree: `
k1: v1
`,
				path: util.Path{"k1", "k2"},
			},
			want:    "",
			found:   false,
			wantErr: false,
		},
		{
			name: "string in map array tree",
			args: args{
				inputTree: `
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - v21
    - v22
`,
				path: util.Path{"a", "b", "name"},
			},
			want: `
n1
`,
			found:   true,
			wantErr: false,
		},
		{
			name: "path not in map array tree",
			args: args{
				inputTree: `
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - v21
    - v22
`,
				path: util.Path{"a", "b", "unknown"},
			},
			want:    ``,
			found:   false,
			wantErr: false,
		},
		{
			name: "node in map array tree",
			args: args{
				inputTree: `
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
`,
				path: util.Path{"a", "b", "list1", "i3b"},
			},
			want: `
list2:
- i1: val1
- i2: val2
- i3a: key1
  i3b:
    i1: va11
`,
			found:   true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := make(map[string]interface{})
			if err := yaml.Unmarshal([]byte(tt.args.inputTree), &tree); err != nil {
				t.Fatal(err)
			}
			got, found, err := tpath.GetFromTreePath(tree, tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetFromTreePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var wantTree interface{}
			if err := yaml.Unmarshal([]byte(tt.want), &wantTree); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, wantTree) {
				t.Errorf("GetFromTreePath() got = %v, want %v", got, tt.want)
			}
			if found != tt.found {
				t.Errorf("GetFromTreePath() found = %v, want %v", found, tt.found)
			}
		})
	}
}
