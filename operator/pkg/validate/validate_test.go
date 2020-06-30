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

package validate

import (
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		desc     string
		yamlStr  string
		wantErrs util.Errors
	}{
		{
			desc: "nil success",
		},
		{
			desc: "complicated k8s overlay",
			yamlStr: `
profile: default
components:
  ingressGateways:
  - enabled: true
    k8s:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: zone
                operator: In
                values:
                - istio`,
		},
		{
			desc: "CommonConfig",
			yamlStr: `
hub: docker.io/istio
tag: v1.2.3
meshConfig:
  rootNamespace: istio-system
values:
  global:
    proxy:
      includeIPRanges: "1.1.0.0/16,2.2.0.0/16"
      excludeIPRanges: "3.3.0.0/16,4.4.0.0/16"

`,
		},
		{
			desc: "BadTag",
			yamlStr: `
hub: ?illegal-tag!
`,
			wantErrs: makeErrors([]string{`invalid value Hub: ?illegal-tag!`}),
		},
		{
			desc: "BadHub",
			yamlStr: `
hub: docker.io:tag/istio
`,
			wantErrs: makeErrors([]string{`invalid value Hub: docker.io:tag/istio`}),
		},
		{
			desc: "BadAddonComponentName",
			yamlStr: `
addonComponents:
  Prometheus:
    enabled: false
`,
			wantErrs: makeErrors([]string{`invalid addon component name: Prometheus, expect component name starting with lower-case character`}),
		},

		{
			desc: "GoodURL",
			yamlStr: `
installPackagePath: /local/file/path
`,
		},
		{
			desc: "BadGatewayName",
			yamlStr: `
components:
  ingressGateways:
  - namespace: istio-ingress-ns2
    name: istio@ingress-1
    enabled: true
`,
			wantErrs: makeErrors([]string{`invalid value Components.IngressGateways[0].Name: istio@ingress-1`}),
		},
		{
			desc: "BadValuesIP",
			yamlStr: `
values:
  global:
    proxy:
      includeIPRanges: "1.1.0.300/16,2.2.0.0/16"
`,
			wantErrs: makeErrors([]string{`global.proxy.includeIPRanges invalid CIDR address: 1.1.0.300/16`}),
		},
		{
			desc: "EmptyValuesIP",
			yamlStr: `
values:
  global:
    proxy:
      includeIPRanges: ""
`,
		},
		{
			desc: "Bad mesh config",
			yamlStr: `
meshConfig:
  defaultConfig:
    discoveryAddress: missingport
`,
			wantErrs: makeErrors([]string{`1 error occurred:
	* invalid discovery address: unable to split "missingport": address missingport: missing port in address

`}),
		},
		{
			desc: "Bad mesh config values",
			yamlStr: `
values:
  meshConfig:
    defaultConfig:
      discoveryAddress: missingport
`,
			wantErrs: makeErrors([]string{`1 error occurred:
	* invalid discovery address: unable to split "missingport": address missingport: missing port in address

`}),
		},
		{
			desc: "Unknown mesh config",
			yamlStr: `
meshConfig:
  foo: bar
`,
			wantErrs: makeErrors([]string{`failed to unmarshall mesh config: unknown field "foo" in v1alpha1.MeshConfig`}),
		},
		{
			desc: "Unknown mesh config values",
			yamlStr: `
values:
  meshConfig:
    foo: bar
`,
			wantErrs: makeErrors([]string{`failed to unmarshall mesh config: unknown field "foo" in v1alpha1.MeshConfig`}),
		},
		{
			desc: "Good mesh config",
			yamlStr: `
meshConfig:
  defaultConfig:
    discoveryAddress: istiod:15012
`,
		},
	}
	if err := name.ScanBundledAddonComponents("../../cmd/mesh/testdata/manifest-generate/data-snapshot"); err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ispec := &v1alpha1.IstioOperatorSpec{}
			err := util.UnmarshalWithJSONPB(tt.yamlStr, ispec, false)
			if err != nil {
				t.Fatalf("unmarshalWithJSONPB(%s): got error %s", tt.desc, err)
			}

			errs := CheckIstioOperatorSpec(ispec, false)
			if gotErrs, wantErrs := errs, tt.wantErrs; !util.EqualErrors(gotErrs, wantErrs) {
				t.Errorf("ProtoToValues(%s)(%v): gotErrs:%s, wantErrs:%s", tt.desc, tt.yamlStr, gotErrs, wantErrs)
			}
		})
	}
}
