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

package translate

import (
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
)

func TestGetEnabledComponents(t *testing.T) {
	tests := []struct {
		desc     string
		yamlStr  string
		want     []string
		wantErrs bool
	}{
		{
			desc:    "nil success",
			yamlStr: "",
			want:    nil,
		},
		{
			desc: "all components disabled",
			yamlStr: `
components:
  pilot:
    enabled: false
  ingressGateways:
  - enabled: false`,
			want: nil,
		},
		{
			desc: "only pilot component enabled",
			yamlStr: `
components:
  pilot:
    enabled: true
  ingressGateways:
  - enabled: false`,
			want: []string{string(name.PilotComponentName)},
		},
		{
			desc: "only gateway component enabled",
			yamlStr: `
components:
  pilot:
    enabled: false
  ingressGateways:
  - enabled: true`,
			want: []string{string(name.IngressComponentName)},
		},
		{
			desc: "all components enabled",
			yamlStr: `
components:
  pilot:
    enabled: true
  ingressGateways:
  - enabled: true`,
			want: []string{string(name.PilotComponentName), string(name.IngressComponentName)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			iopSpec := &v1alpha1.IstioOperatorSpec{}
			err := util.UnmarshalWithJSONPB(tt.yamlStr, iopSpec, false)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s", tt.desc, err)
			}

			enabledComponents, err := GetEnabledComponents(iopSpec)
			if err != nil {
				t.Errorf("GetEnabledComponents(%s)(%v): got error: %v", tt.desc, tt.yamlStr, err)
			}
			if !testEquality(enabledComponents, tt.want) {
				t.Errorf("GetEnabledComponents(%s)(%v): got: %v, want: %v", tt.desc, tt.yamlStr, enabledComponents, tt.want)
			}
		})
	}
}

func testEquality(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
