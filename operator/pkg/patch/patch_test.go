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

package patch

import (
	"fmt"
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/util"
)

func TestPatchYAMLManifestSuccess(t *testing.T) {
	base := `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list: 
    - v1
    - v2
    - v3_regex
  c:
`
	tests := []struct {
		desc    string
		path    string
		value   string
		want    string
		wantErr string
	}{
		{
			desc:  "ModifyListEntryValue",
			path:  `a.b.[name:n1].value`,
			value: `v2`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v2
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
  c:
`,
		},
		{
			desc:  "ModifyListEntryValueQuoted",
			path:  `a.b.[name:n1].value`,
			value: `v2`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: "n1"
    value: v2
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
  c:
`,
		},
		{
			desc:  "ModifyListEntry",
			path:  `a.b.[name:n2].list.[v2]`,
			value: `v3`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v3
    - v3_regex
    name: n2
  c:
`,
		},
		{
			desc: "DeleteListEntry",
			path: `a.b.[name:n1]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - list:
    - v1
    - v2
    - v3_regex
    name: n2
  c:
`,
		},
		{
			desc: "DeleteListEntryValue",
			path: `a.b.[name:n2].list.[v2]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v3_regex
    name: n2
  c:
`,
		},
		{
			desc: "DeleteListEntryValueRegex",
			path: `a.b.[name:n2].list.[v3]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v2
    name: n2
  c:
`,
		},
		{
			desc: "UpdateNullNode",
			path: `a.c`,
			value: `
      d: n3`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list: 
    - v1
    - v2
    - v3_regex
  c:
    d: n3
`,
		},
		{
			desc:  "AppendToListEntry",
			path:  `a.b.[name:n2].list.[3]`,
			value: `v4`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v2
    - v3_regex
    - v4
    name: n2
  c:
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rc := &v1alpha1.KubernetesResourcesSpec{}
			oh := makeOverlayHeader(tt.path, tt.value)
			err := util.UnmarshalWithJSONPB(oh, rc, false)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s for string:\n%s\n", tt.desc, err, oh)
			}
			got, err := YAMLManifestPatch(base, "istio-system", rc.Overlays)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Fatalf("YAMLManifestPatch(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if want := tt.want; !util.IsYAMLEqual(got, want) {
				t.Errorf("YAMLManifestPatch(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

func TestPatchYAMLManifestRealYAMLSuccess(t *testing.T) {
	base := `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - name: deleteThis
        foo: bar
      - name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
        - containerPort: 9901
        command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        - --validation-webhook-config-file
`

	tests := []struct {
		desc    string
		path    string
		value   string
		want    string
		wantErr string
	}{
		{
			desc: "DeleteLeafListLeaf",
			path: `spec.template.spec.containers.[name:galley].command.[--validation-webhook-config-file]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
        - containerPort: 9901
`,
		},
		{
			desc:  "UpdateListItem",
			path:  `spec.template.spec.containers.[name:galley].command.[--livenessProbeInterval]`,
			value: `--livenessProbeInterval=1111s`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1111s
        - --validation-webhook-config-file
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
        - containerPort: 9901
`,
		},
		{
			desc:  "UpdateLeaf",
			path:  `spec.template.spec.containers.[name:galley].ports.[containerPort:15014].containerPort`,
			value: `22222`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        - --validation-webhook-config-file
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 22222
        - containerPort: 9901
`,
		},
		{
			desc: "DeleteLeafList",
			path: `spec.template.spec.containers.[name:galley].ports.[containerPort:9901]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        - --validation-webhook-config-file
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
`,
		},
		{
			desc: "DeleteInternalNode",
			path: `spec.template.spec.containers.[name:deleteThis]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        - --validation-webhook-config-file
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
        - containerPort: 9901
`,
		},
		{
			desc: "DeleteLeafListEntry",
			path: `spec.template.spec.containers.[name:galley].command.[--validation-webhook-config-file]`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        name: galley
        ports:
        - containerPort: 443
        - containerPort: 15014
        - containerPort: 9901
`,
		},
		{
			desc: "UpdateInteriorNode",
			path: `spec.template.spec.containers.[name:galley].ports.[containerPort:15014]`,
			value: `
      fooPort: 15015`,
			want: `
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
spec:
  template:
    spec:
      containers:
      - foo: bar
        name: deleteThis
      - command:
        - /usr/local/bin/galley
        - server
        - --meshConfigFile=/etc/mesh-config/mesh
        - --livenessProbeInterval=1s
        - --validation-webhook-config-file
        name: galley
        ports:
        - containerPort: 443
        - fooPort: 15015
        - containerPort: 9901

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rc := &v1alpha1.KubernetesResourcesSpec{}
			t.Log(makeOverlayHeader(tt.path, tt.value))
			err := util.UnmarshalWithJSONPB(makeOverlayHeader(tt.path, tt.value), rc, false)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s", tt.desc, err)
			}
			got, err := YAMLManifestPatch(base, "istio-system", rc.Overlays)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Fatalf("YAMLManifestPatch(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if want := tt.want; !util.IsYAMLEqual(got, want) {
				t.Errorf("YAMLManifestPatch(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

func makeOverlayHeader(path, value string) string {
	const (
		patchCommon = `overlays:
- kind: Deployment
  name: istio-citadel
  patches:
  - path: `
		valueStr = `    value: `
	)

	ret := patchCommon
	ret += fmt.Sprintf("%s\n", path)
	if value != "" {
		ret += fmt.Sprintf("%s%s\n", valueStr, value)
	}
	return ret
}

// errToString returns the string representation of err and the empty string if
// err is nil.
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
