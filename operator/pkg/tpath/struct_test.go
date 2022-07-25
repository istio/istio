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
	"testing"

	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/util"
)

func TestGetFromStructPath(t *testing.T) {
	tests := []struct {
		desc      string
		nodeYAML  string
		path      string
		wantYAML  string
		wantFound bool
		wantErr   string
	}{
		{
			desc: "GetStructItem",
			nodeYAML: `
a: va
b: vb
c:
  d: vd
  e:
    f: vf
g:
  h:
  - i: vi
    j: vj
    k:
      l:
        m: vm
        n: vn
`,
			path: "c",
			wantYAML: `
d: vd
e:
  f: vf
`,
			wantFound: true,
		},
		{
			desc: "GetSliceEntryItem",
			nodeYAML: `
a: va
b: vb
c:
  d: vd
  e:
    f: vf
g:
  h:
  - i: vi
    j: vj
    k:
      l:
        m: vm
        n: vm
`,
			path: "g.h.0",
			wantYAML: `
i: vi
j: vj
k:
  l:
    m: vm
    n: vm
`,
			wantFound: true,
		},
		{
			desc: "GetMapEntryItem",
			nodeYAML: `
a: va
b: vb
c:
  d: vd
  e:
    f: vf
g:
  h:
  - i: vi
    j: vj
    k:
      l:
        m: vm
        n: vm
`,
			path: "g.h.0.k",
			wantYAML: `
l:
  m: vm
  n: vm
`,
			wantFound: true,
		},
		{
			desc: "GetPathNotExists",
			nodeYAML: `
a: va
b: vb
c:
  d: vd
  e:
    f: vf
g:
  h:
  - i: vi
    j: vj
    k:
      l:
        m: vm
        n: vm
`,
			path:      "c.d.e",
			wantFound: false,
			wantErr:   "getFromStructPath path e, unsupported type string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rnode := make(map[string]any)
			if err := yaml.Unmarshal([]byte(tt.nodeYAML), &rnode); err != nil {
				t.Fatal(err)
			}
			GotOut, GotFound, gotErr := GetFromStructPath(rnode, tt.path)
			if GotFound != tt.wantFound {
				t.Fatalf("GetFromStructPath(%s): gotFound:%v, wantFound:%v", tt.desc, GotFound, tt.wantFound)
			}
			if gotErr, wantErr := errToString(gotErr), tt.wantErr; gotErr != wantErr {
				t.Fatalf("GetFromStructPath(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if tt.wantErr != "" || !tt.wantFound {
				return
			}
			gotYAML := util.ToYAML(GotOut)
			diff := util.YAMLDiff(gotYAML, tt.wantYAML)
			if diff != "" {
				t.Errorf("GetFromStructPath(%s): YAML of gotOut:\n%s\n, YAML of wantOut:\n%s\n, diff:\n%s\n", tt.desc, gotYAML, tt.wantYAML, diff)
			}
		})
	}
}
