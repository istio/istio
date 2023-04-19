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

	"sigs.k8s.io/yaml"

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
			tree := make(map[string]any)
			if err := yaml.Unmarshal([]byte(tt.args.inputTree), &tree); err != nil {
				t.Fatal(err)
			}
			got, found, err := tpath.Find(tree, tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Find() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var wantTree any
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

func TestManifestMap_Consolidated(t *testing.T) {
	tests := []struct {
		name string
		mm   ManifestMap
		want map[string]string
	}{
		{
			name: "consolidate output from manifest map",
			mm: ManifestMap{
				"key1": []string{"value1", "value2"},
				"key2": []string{},
			},
			want: map[string]string{
				"key1": "value1\n---\nvalue2\n---\n",
				"key2": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mm.Consolidated(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ManifestMap.Consolidated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeManifestSlices(t *testing.T) {
	tests := []struct {
		name      string
		manifests []string
		want      string
	}{
		{
			name:      "merge manifest slices",
			manifests: []string{"key1", "key2", "key3"},
			want:      "key1\n---\nkey2\n---\nkey3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeManifestSlices(tt.manifests); got != tt.want {
				t.Errorf("MergeManifestSlices() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManifestMap_String(t *testing.T) {
	tests := []struct {
		name  string
		mm    ManifestMap
		want1 string
		want2 string
	}{
		{
			name: "consolidate from manifest map",
			mm: ManifestMap{
				"key1": []string{"value1", "value2"},
				"key2": []string{"value2", "value3"},
			},
			want1: "value1\n---\nvalue2\n---\nvalue2\n---\nvalue3\n---\n",
			want2: "value2\n---\nvalue3\n---\nvalue1\n---\nvalue2\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mm.String()
			if got != tt.want1 && got != tt.want2 {
				t.Errorf("ManifestMap.String() = %v, want %v or %v", got, tt.want1, tt.want2)
			}
		})
	}
}

func TestComponentName_IsGateway(t *testing.T) {
	tests := []struct {
		name string
		cn   ComponentName
		want bool
	}{
		{
			name: "ComponentName is IngressGateways",
			cn:   IngressComponentName,
			want: true,
		},
		{
			name: "ComponentName is EgressGateways",
			cn:   EgressComponentName,
			want: true,
		},
		{
			name: "ComponentName is others",
			cn:   CNIComponentName,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cn.IsGateway(); got != tt.want {
				t.Errorf("ComponentName.IsGateway() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		name string
		n    ComponentName
		want ComponentName
	}{
		{
			name: "to upper title",
			n:    "ingressGateways",
			want: "IngressGateways",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TitleCase(tt.n); got != tt.want {
				t.Errorf("TitleCase() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserFacingComponentName(t *testing.T) {
	tests := []struct {
		name string
		n    ComponentName
		want string
	}{
		{
			name: "ComponentName is unknown",
			n:    "foo",
			want: "Unknown",
		},
		{
			name: "ComponentName is  istio core",
			n:    IstioBaseComponentName,
			want: "Istio core",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UserFacingComponentName(tt.n); got != tt.want {
				t.Errorf("UserFacingComponentName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamespace(t *testing.T) {
	type args struct {
		componentName    ComponentName
		controlPlaneSpec *v1alpha1.IstioOperatorSpec
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "DefaultNamespace and componentNamespace are unset",
			args: args{
				componentName: CNIComponentName,
				controlPlaneSpec: &v1alpha1.IstioOperatorSpec{
					Hub: "docker.io",
				},
			},
			want:    "",
			wantErr: false,
		},
		{
			name: "DefaultNamespace is set and componentNamespace in empty",
			args: args{
				componentName: CNIComponentName,
				controlPlaneSpec: &v1alpha1.IstioOperatorSpec{
					Hub:       "docker.io",
					Namespace: "istio-system",
					Components: &v1alpha1.IstioComponentSetSpec{
						Cni: &v1alpha1.ComponentSpec{
							Namespace: "",
						},
					},
				},
			},
			want:    "istio-system",
			wantErr: false,
		},
		{
			name: "DefaultNamespace is set and componentNamespace is unset",
			args: args{
				componentName: CNIComponentName,
				controlPlaneSpec: &v1alpha1.IstioOperatorSpec{
					Hub:       "docker.io",
					Namespace: "istio-system",
					Components: &v1alpha1.IstioComponentSetSpec{
						Cni: &v1alpha1.ComponentSpec{},
					},
				},
			},
			want:    "istio-system",
			wantErr: false,
		},
		{
			name: "DefaultNamespace and componentNamespace are set and not empty",
			args: args{
				componentName: CNIComponentName,
				controlPlaneSpec: &v1alpha1.IstioOperatorSpec{
					Hub:       "docker.io",
					Namespace: "istio-system",
					Components: &v1alpha1.IstioComponentSetSpec{
						Cni: &v1alpha1.ComponentSpec{
							Namespace: "istio-test",
						},
					},
				},
			},
			want:    "istio-test",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Namespace(tt.args.componentName, tt.args.controlPlaneSpec)
			if (err != nil) != tt.wantErr {
				t.Errorf("Namespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Namespace() = %v, want %v", got, tt.want)
			}
		})
	}
}
