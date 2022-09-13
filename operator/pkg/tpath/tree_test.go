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
		value     any
		want      string
		wantFound bool
		wantErr   string
	}{
		{
			desc:      "AddListEntry",
			path:      `a.b.[name:n2].list`,
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
    - foo
`,
		},
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
			path:      `a.b.[name:n2].list.[:v2]`,
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
			desc: "ModifyListEntryMapValue",
			path: `a.b.[name:n2]`,
			value: `name: n2
list: 
  - nk1: nv1
  - nk2: nv2`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - name: n2
    list:
    - nk1: nv1
    - nk2: nv2
`,
		},
		{
			desc:      "ModifyNthListEntry",
			path:      `a.b.[1].list.[:v2]`,
			value:     `v-the-second`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v-the-second
    - v3_regex
    name: n2
`,
		},
		{
			desc:      "ModifyNthLeafListEntry",
			path:      `a.b.[1].list.[2]`,
			value:     `v-the-third`,
			wantFound: true,
			want: `
a:
  b:
  - name: n1
    value: v1
  - list:
    - v1
    - v2
    - v-the-third
    name: n2
`,
		},
		{
			desc:      "ModifyListEntryValueDotless",
			path:      `a.b[name:n1].value`,
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
			path:      `a.b.[name:n2].list.[:v2]`,
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
			desc:      "DeleteListEntryIndex",
			path:      `a.b.[name:n2].list.[1]`,
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
			path:      `a.b.[name:n2].list.[:v3]`,
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
			desc:      "DeleteListLeafEntryBogusIndex",
			path:      `a.b.[name:n2].list.[-200]`,
			wantFound: false,
			wantErr:   `path a.b.[name:n2].list.[-200]: element [-200] not found`,
		},
		{
			desc:      "DeleteListEntryBogusIndex",
			path:      `a.b.[1000000].list.[:v2]`,
			wantFound: false,
			wantErr:   `index 1000000 exceeds list length 2 at path [1000000].list.[:v2]`,
		},
		{
			desc:      "AddMapEntry",
			path:      `a.new_key`,
			value:     `new_val`,
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
  new_key: new_val
`,
		},
		{
			desc: "AddMapEntryMapValue",
			path: `a.new_key`,
			value: `new_key:
  nk1:
    nk2: nv2`,
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
  new_key:
    nk1:
      nk2: nv2
`,
		},
		{
			desc: "ModifyMapEntryMapValue",
			path: `a.b`,
			value: `nk1:
  nk2: nv2`,
			wantFound: true,
			want: `
a:
  nk1:
    nk2: nv2
`,
		},
		{
			desc:      "DeleteMapEntry",
			path:      `a.b`,
			wantFound: true,
			want: `
a: {}
`,
		},
		{
			desc:      "path not found",
			path:      `a.c.[name:n2].list.[:v3]`,
			wantFound: false,
			wantErr:   `path not found at element c in path a.c.[name:n2].list.[:v3]`,
		},
		{
			desc:      "error key",
			path:      `a.b.[].list`,
			wantFound: false,
			wantErr:   `path a.b.[].list: [] is not a valid key:value path element`,
		},
		{
			desc:      "invalid index",
			path:      `a.c.[n2].list.[:v3]`,
			wantFound: false,
			wantErr:   `path not found at element c in path a.c.[n2].list.[:v3]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]any)
			if err := yaml.Unmarshal([]byte(rootYAML), &root); err != nil {
				t.Fatal(err)
			}
			pc, gotFound, gotErr := GetPathContext(root, util.PathFromString(tt.path), false)
			if gotErr, wantErr := errToString(gotErr), tt.wantErr; gotErr != wantErr {
				t.Fatalf("GetPathContext(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if gotFound != tt.wantFound {
				t.Fatalf("GetPathContext(%s): gotFound:%v, wantFound:%v", tt.desc, gotFound, tt.wantFound)
			}
			if tt.wantErr != "" || !tt.wantFound {
				if tt.want != "" {
					t.Error("tt.want is set but never checked")
				}
				return
			}

			err := WritePathContext(pc, tt.value, false)
			if err != nil {
				t.Fatal(err)
			}

			gotYAML := util.ToYAML(root)
			diff := util.YAMLDiff(gotYAML, tt.want)
			if diff != "" {
				t.Errorf("%s: (got:-, want:+):\n%s\n", tt.desc, diff)
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
		},
		// For https://github.com/istio/istio/issues/20950
		{
			desc: "with initial list",
			baseYAML: `
components:
  ingressGateways:
    - enabled: true
`,
			path:  "components.ingressGateways[0].enabled",
			value: "false",
			want: `
components:
  ingressGateways:
    - enabled: "false"
`,
		},
		{
			desc:     "no initial list",
			baseYAML: "",
			path:     "components.ingressGateways[0].enabled",
			value:    "false",
			want: `
components:
  ingressGateways:
    - enabled: "false"
`,
		},
		{
			desc: "no initial list for entry",
			baseYAML: `
a: {}
`,
			path:  "a.list.[0]",
			value: "v1",
			want: `
a:
  list:
    - v1		
`,
		},
		{
			desc: "ExtendNthLeafListEntry",
			baseYAML: `
a:
  list:
    - v1
`,
			path:  `a.list.[1]`,
			value: `v2`,
			want: `
a:
  list: 
  - v1
  - v2
`,
		},
		{
			desc: "ExtendLeafListEntryLargeIndex",
			baseYAML: `
a:
  list:
    - v1
`,
			path:  `a.list.[999]`,
			value: `v2`,
			want: `
a:
  list:
  - v1
  - v2
`,
		},
		{
			desc: "ExtendLeafListEntryNegativeIndex",
			baseYAML: `
a:
  list:
    - v1
`,
			path:  `a.list.[-1]`,
			value: `v2`,
			want: `
a:
  list:
  - v1
  - v2
`,
		},
		{
			desc: "ExtendNthListEntry",
			baseYAML: `
a:
  list:
  - name: foo
`,
			path:  `a.list.[1].name`,
			value: `bar`,
			want: `
a:
  list:
  - name: foo
  - name: bar
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]any)
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

func TestMergeNode(t *testing.T) {
	testTreeYAML := `
a:
  b:
    c: val1
    list1:
    - i1: val1
    - i2: val2
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
			desc:     "merge list entry",
			baseYAML: testTreeYAML,
			path:     "a.b.list1.[i1:val1]",
			value: `
i2b: val2`,
			want: `
a:
  b:
    c: val1
    list1:
    - i1: val1
      i2b: val2
    - i2: val2
`,
		},
		{
			desc:     "merge list 2",
			baseYAML: testTreeYAML,
			path:     "a.b.list1",
			value: `
i3:
  a: val3
`,
			want: `
a:
  b:
    c: val1
    list1:
    - i1: val1
    - i2: val2
    - i3:
        a: val3
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]any)
			if tt.baseYAML != "" {
				if err := yaml.Unmarshal([]byte(tt.baseYAML), &root); err != nil {
					t.Fatal(err)
				}
			}
			p := util.PathFromString(tt.path)
			iv := make(map[string]any)
			err := yaml.Unmarshal([]byte(tt.value), &iv)
			if err != nil {
				t.Fatal(err)
			}
			err = MergeNode(root, p, iv)
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

// TestSecretVolumes simulates https://github.com/istio/istio/issues/20381
func TestSecretVolumes(t *testing.T) {
	rootYAML := `
values:
   gateways:
      istio-egressgateway:
         secretVolumes: []
`
	root := make(map[string]any)
	if err := yaml.Unmarshal([]byte(rootYAML), &root); err != nil {
		t.Fatal(err)
	}
	overrides := []struct {
		path  string
		value any
	}{
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[0].name",
			value: "egressgateway-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[0].secretName",
			value: "istio-egressgateway-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[0].mountPath",
			value: "/etc/istio/egressgateway-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[1].name",
			value: "egressgateway-ca-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[1].secretName",
			value: "istio-egressgateway-ca-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[1].mountPath",
			value: "/etc/istio/egressgateway-ca-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[2].name",
			value: "nginx-client-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[2].secretName",
			value: "nginx-client-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[2].mountPath",
			value: "/etc/istio/nginx-client-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[3].name",
			value: "nginx-ca-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[3].secretName",
			value: "nginx-ca-certs",
		},
		{
			path:  "values.gateways.istio-egressgateway.secretVolumes[3].mountPath",
			value: "/etc/istio/nginx-ca-certs",
		},
	}

	for _, override := range overrides {

		pc, _, err := GetPathContext(root, util.PathFromString(override.path), true)
		if err != nil {
			t.Fatalf("GetPathContext(%q): %v", override.path, err)
		}
		err = WritePathContext(pc, override.value, false)
		if err != nil {
			t.Fatalf("WritePathContext(%q): %v", override.path, err)
		}
	}

	want := `
values:
   gateways:
      istio-egressgateway:
         secretVolumes:
         - mountPath: /etc/istio/egressgateway-certs
           name: egressgateway-certs
           secretName: istio-egressgateway-certs
         - mountPath: /etc/istio/egressgateway-ca-certs
           name: egressgateway-ca-certs
           secretName: istio-egressgateway-ca-certs
         - mountPath: /etc/istio/nginx-client-certs
           name: nginx-client-certs
           secretName: nginx-client-certs
         - mountPath: /etc/istio/nginx-ca-certs
           name: nginx-ca-certs
           secretName: nginx-ca-certs
`
	gotYAML := util.ToYAML(root)
	diff := util.YAMLDiff(gotYAML, want)
	if diff != "" {
		t.Errorf("TestSecretVolumes: diff:\n%s\n", diff)
	}
}

// Simulates https://github.com/istio/istio/issues/19196
func TestWriteEscapedPathContext(t *testing.T) {
	rootYAML := `
values:
  sidecarInjectorWebhook:
    injectedAnnotations: {}
`
	tests := []struct {
		desc      string
		path      string
		value     any
		want      string
		wantFound bool
		wantErr   string
	}{
		{
			desc:      "ModifyEscapedPathValue",
			path:      `values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy`,
			value:     `runtime/default`,
			wantFound: true,
			want: `
values:
  sidecarInjectorWebhook:
    injectedAnnotations:
      container.apparmor.security.beta.kubernetes.io/istio-proxy: runtime/default
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]any)
			if err := yaml.Unmarshal([]byte(rootYAML), &root); err != nil {
				t.Fatal(err)
			}
			pc, gotFound, gotErr := GetPathContext(root, util.PathFromString(tt.path), false)
			if gotErr, wantErr := errToString(gotErr), tt.wantErr; gotErr != wantErr {
				t.Fatalf("GetPathContext(%s): gotErr:%s, wantErr:%s", tt.desc, gotErr, wantErr)
			}
			if gotFound != tt.wantFound {
				t.Fatalf("GetPathContext(%s): gotFound:%v, wantFound:%v", tt.desc, gotFound, tt.wantFound)
			}
			if tt.wantErr != "" || !tt.wantFound {
				return
			}

			err := WritePathContext(pc, tt.value, false)
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
