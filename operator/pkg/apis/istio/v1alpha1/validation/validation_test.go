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

package validation_test

import (
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1/validation"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test/env"
)

const operatorSubdirFilePath = "manifests"

// nolint: lll
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name     string
		value    *v1alpha1.IstioOperatorSpec
		values   string
		errors   string
		warnings string
	}{
		{
			name: "unset target port",
			values: `
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
    - name: cluster-local-gateway
      enabled: true
      k8s:
        service:
          type: ClusterIP
          ports:
          - port: 15020
            name: status-port
          - port: 80
            name: http2
`,
			errors: `port http2/80 in gateway cluster-local-gateway invalid: targetPort is set to 0, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`,
		},
		{
			name: "explicitly invalid target port",
			values: `
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
    - name: cluster-local-gateway
      enabled: true
      k8s:
        service:
          type: ClusterIP
          ports:
          - port: 15020
            name: status-port
          - port: 80
            name: http2
            targetPort: 90
`,
			errors: `port http2/80 in gateway cluster-local-gateway invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`,
		},
		{
			name: "explicitly invalid target port for egress",
			values: `
components:
  egressGateways:
    - name: egress-gateway
      enabled: true
      k8s:
        service:
          type: ClusterIP
          ports:
          - port: 15020
            name: status-port
          - port: 80
            name: http2
            targetPort: 90
`,
			errors: `port http2/80 in gateway egress-gateway invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-egressgateway.runAsRoot=true`,
		},
		{
			name: "low target port with root",
			values: `
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
    - name: cluster-local-gateway
      enabled: true
      k8s:
        service:
          type: ClusterIP
          ports:
          - port: 15020
            name: status-port
          - port: 80
            name: http2
            targetPort: 90
values:
  gateways:
    istio-ingressgateway:
      runAsRoot: true
`,
			errors: ``,
		},
		{
			name: "legacy values ports config empty targetPort",
			values: `
values:
  gateways:
    istio-ingressgateway:
      ingressPorts:
      - name: http
        port: 80
`,
			errors: `port 80 is invalid: targetPort is set to 0, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`,
		},
		{
			name: "legacy values ports config explicit targetPort",
			values: `
values:
  gateways:
    istio-ingressgateway:
      ingressPorts:
      - name: http
        port: 80
        targetPort: 90
`,
			errors: `port 80 is invalid: targetPort is set to 90, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true`,
		},
		{
			name: "legacy values ports valid",
			values: `
values:
  gateways:
    istio-ingressgateway:
      ingressPorts:
      - name: http
        port: 80
        targetPort: 8080
`,
			errors: ``,
		},
		{
			name: "replicaCount set when autoscaleEnabled is true",
			values: `
values:
  pilot:
    autoscaleEnabled: true
  gateways:
    istio-ingressgateway:
      autoscaleEnabled: true
    istio-egressgateway:
      autoscaleEnabled: true
components:
  pilot:
    k8s:
      replicaCount: 2
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        replicaCount: 2
  egressGateways:
    - name: istio-egressgateway
      enabled: true
      k8s:
        replicaCount: 2
`,
			warnings: strings.TrimSpace(`
components.pilot.k8s.replicaCount should not be set when values.pilot.autoscaleEnabled is true
components.ingressGateways[name=istio-ingressgateway].k8s.replicaCount should not be set when values.gateways.istio-ingressgateway.autoscaleEnabled is true
components.egressGateways[name=istio-egressgateway].k8s.replicaCount should not be set when values.gateways.istio-egressgateway.autoscaleEnabled is true
`),
		},
		{
			name: "pilot.k8s.replicaCount is default value set when autoscaleEnabled is true",
			values: `
values:
  pilot:
    autoscaleEnabled: true
  gateways:
    istio-ingressgateway:
      autoscaleEnabled: true
    istio-egressgateway:
      autoscaleEnabled: true
components:
  pilot:
    k8s:
      replicaCount: 1
`,
			warnings: strings.TrimSpace(``),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iop := tt.value
			if tt.values != "" {
				iop = &v1alpha1.IstioOperatorSpec{}
				if err := util.UnmarshalWithJSONPB(tt.values, iop, true); err != nil {
					t.Fatal(err)
				}
			}
			err, warnings := validation.ValidateConfig(iop)
			if tt.errors != err.String() {
				t.Fatalf("expected errors: \n%q\n got: \n%q\n", tt.errors, err.String())
			}
			if tt.warnings != warnings {
				t.Fatalf("expected warnings: \n%q\n got \n%q\n", tt.warnings, warnings)
			}
		})
	}
}

func TestValidateProfiles(t *testing.T) {
	manifests := filepath.Join(env.IstioSrc, operatorSubdirFilePath)
	profiles, err := helm.ListProfiles(manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) < 2 {
		// Just ensure we find some profiles, in case this code breaks
		t.Fatalf("Maybe have failed getting profiles, got %v", profiles)
	}
	for _, tt := range profiles {
		t.Run(tt, func(t *testing.T) {
			//_, s, err := manifest.GenerateIstioOperatorWithProfile(tt, "", []string{"installPackagePath=" + manifests}, false, false, nil, l)
			//if err != nil {
			//	t.Fatal(err)
			//}
			// TODO
			verr, warnings := validation.ValidateConfig(nil)
			if verr != nil {
				t.Fatalf("got error validating: %v", verr)
			}
			if warnings != "" {
				t.Fatalf("got warning validating: %v", warnings)
			}
		})
	}
}
