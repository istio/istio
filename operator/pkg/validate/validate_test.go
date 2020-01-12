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

package validate

import (
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/operator/pkg/util"
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
			desc: "SidecarInjectorConfig",
			yamlStr: `
meshConfig:
  rootNamespace: istio-system
components:
  sidecarInjector:
    enabled: true
`,
		},
		{
			desc: "CommonConfig",
			// TODO:        debug: INFO
			yamlStr: `
hub: docker.io/istio
tag: v1.2.3
meshConfig:
  rootNamespace: istio-system
components:
  proxy:
    enabled: true
    namespace: istio-control-system
    k8s:
      resources:
        requests:
          memory: "64Mi"
          cpu: "250m"
        limits:
          memory: "128Mi"
          cpu: "500m"
      readinessProbe:
        httpGet:
          path: /ready
          port: 8080
        initialDelaySeconds: 11
        periodSeconds: 22
        successThreshold: 33
        failureThreshold: 44
      hpaSpec:
        scaleTargetRef:
          apiVersion: apps/v1
          kind: Deployment
          name: php-apache
        minReplicas: 1
        maxReplicas: 10
        metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
      nodeSelector:
        disktype: ssd
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
			desc: "GoodURL",
			yamlStr: `
installPackagePath: /local/file/path
`,
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
      includeInboundPorts: "*"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ispec := &v1alpha1.IstioOperatorSpec{}
			err := util.UnmarshalWithJSONPB(tt.yamlStr, ispec)
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
