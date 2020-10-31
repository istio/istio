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

package name

import (
	"reflect"
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
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
k1:
  k2: v1
`,
				path: util.Path{"k1", "k2", "k3"},
			},
			want:    "",
			found:   false,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := make(map[string]interface{})
			if err := yaml.Unmarshal([]byte(tt.args.inputTree), &tree); err != nil {
				t.Fatal(err)
			}
			got, found, err := tpath.Find(tree, tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Find() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var wantTree interface{}
			if err := yaml.Unmarshal([]byte(tt.want), &wantTree); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, wantTree) {
				t.Errorf("Find() got = %v, want %v", got, tt.want)
			}
			if found != tt.found {
				t.Errorf("Find() found = %v, want %v", found, tt.found)
			}
		})
	}
}

func TestRootNamespaceForMeshConfig(t *testing.T) {
	tests := []struct {
		desc              string
		yamlStr           string
		wantRootNamespace string
	}{
		{
			desc: "only iopSpec.Namespace",
			yamlStr: `
namespace: istio-foo`,
			wantRootNamespace: "istio-foo",
		},
		{
			desc: "only iopSpec.Values[global].istioNamespace",
			yamlStr: `
values:
  global:
    istioNamespace: istio-foo`,
			wantRootNamespace: "istio-foo",
		},
		{
			desc: "only iopSpec.Values[meshConfig].rootNamespace",
			yamlStr: `
values:
  meshConfig:
    rootNamespace: istio-foo`,
			wantRootNamespace: "istio-foo",
		},
		{
			desc: "only iopSpec.MeshConfig.rootNamespace",
			yamlStr: `
meshConfig:
  rootNamespace: istio-foo`,
			wantRootNamespace: "istio-foo",
		},
		{
			desc: "iopSpec.Values[meshConfig].rootNamespace and iopSpec.Values[meshConfig].rootNamespace",
			yamlStr: `
values:
  global:
    istioNamespace: istio-foo
  meshConfig:
    rootNamespace: istio-bar`,
			wantRootNamespace: "istio-bar",
		},
		{
			desc: "iopSpec.Values[meshConfig].rootNamespace and iopSpec.MeshConfig.rootNamespace",
			yamlStr: `
meshConfig:
  rootNamespace: istio-bar
values:
  global:
    istioNamespace: istio-foo`,
			wantRootNamespace: "istio-bar",
		},
		{
			desc: "iopSpec.Namespace, iopSpec.Values[meshConfig].rootNamespace and iopSpec.Values[meshConfig].rootNamespace",
			yamlStr: `
namespace: istio-foo
values:
  global:
    istioNamespace: istio-foo
  meshConfig:
    rootNamespace: istio-bar`,
			wantRootNamespace: "istio-bar",
		},
		{
			desc: "iopSpec.Namespace, iopSpec.Values[meshConfig].rootNamespace and iopSpec.MeshConfig.rootNamespace",
			yamlStr: `
namespace: istio-foo
meshConfig:
  rootNamespace: istio-bar
values:
  global:
    istioNamespace: istio-foo`,
			wantRootNamespace: "istio-bar",
		},
		{
			desc: "iopSpec.MeshConfig.rootNamespace and iopSpec.Values[meshConfig].rootNamespace",
			yamlStr: `
meshConfig:
  rootNamespace: istio-bar
values:
  global:
    istioNamespace: istio-foo
  meshConfig:
    rootNamespace: istio-bar`,
			wantRootNamespace: "istio-bar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ispec := &v1alpha1.IstioOperatorSpec{}
			err := util.UnmarshalWithJSONPB(tt.yamlStr, ispec, false)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s", tt.yamlStr, err)
			}
			gotRootNamespace := RootNamespaceForMeshConfig(ispec)
			if gotRootNamespace != tt.wantRootNamespace {
				t.Errorf("%s: wanted rootNamespace: %s, got rootNamespace: %s", tt.desc, tt.wantRootNamespace, gotRootNamespace)
			}
		})
	}
}
