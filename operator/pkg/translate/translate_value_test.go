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

package translate

import (
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/kr/pretty"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/version"
)

func TestValueToProto(t *testing.T) {
	tests := []struct {
		desc      string
		valueYAML string
		want      string
		wantErr   string
	}{
		{
			desc: "K8s resources translation",
			valueYAML: `
pilot:
  enabled: true
  rollingMaxSurge: 100%
  rollingMaxUnavailable: 25%
  resources:
    requests:
      cpu: 1000m
      memory: 1G
  replicaCount: 1
  nodeSelector:
    beta.kubernetes.io/os: linux
  tolerations:
  - key: dedicated
    operator: Exists
    effect: NoSchedule
  - key: CriticalAddonsOnly
    operator: Exists
  autoscaleEnabled: true
  autoscaleMax: 3
  autoscaleMin: 1
  cpu:
    targetAverageUtilization: 80
  traceSampling: 1.0
  image: pilot
  env:
    GODEBUG: gctrace=1
  podAntiAffinityLabelSelector:
  - key: istio
    operator: In
    values: pilot
    topologyKey: "kubernetes.io/hostname"
global:
  hub: docker.io/istio
  istioNamespace: istio-system
  policyNamespace: istio-policy
  tag: 1.2.3
  telemetryNamespace: istio-telemetry
  proxy:
    readinessInitialDelaySeconds: 2
  controlPlaneSecurityEnabled: false
mixer:
  policy:
    enabled: true
    image: mixer
    replicaCount: 1
  telemetry:
    enabled: false
`,
			want: `
hub: docker.io/istio
tag: 1.2.3
meshConfig: 
   rootNamespace: istio-system
components:
   telemetry:
     enabled: false
   policy:
     enabled: true
     k8s:
       replicaCount: 1
   pilot:
     enabled: true
     k8s:
       replicaCount: 1
       env:
       - name: GODEBUG
         value: gctrace=1
       hpaSpec:
          maxReplicas: 3
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
           - resource:
               name: cpu
               targetAverageUtilization: 80
             type: Resource
       nodeSelector:
          beta.kubernetes.io/os: linux
       tolerations:
       - key: dedicated
         operator: Exists
         effect: NoSchedule
       - key: CriticalAddonsOnly
         operator: Exists
       resources:
          requests:
            cpu: 1000m
            memory: 1G
       strategy:
         rollingUpdate:
           maxSurge: 100%
           maxUnavailable: 25%
values:
  global:
    controlPlaneSecurityEnabled: false
    proxy:
      readinessInitialDelaySeconds: 2
    policyNamespace: istio-policy
    telemetryNamespace: istio-telemetry
  pilot:
    image: pilot
    autoscaleEnabled: true
    traceSampling: 1
    podAntiAffinityLabelSelector:
    - key: istio
      operator: In
      values: pilot
      topologyKey: "kubernetes.io/hostname"
  mixer:
    policy:
      image: mixer
`,
		},
		{
			desc: "All Enabled",
			valueYAML: `
global:
  hub: docker.io/istio
  istioNamespace: istio-system
  policyNamespace: istio-policy
  tag: 1.2.3
  telemetryNamespace: istio-telemetry
mixer:
  policy:
    enabled: true
  telemetry:
    enabled: true
pilot:
  enabled: true
istiocoredns:
  enabled: true
gateways:
  enabled: true
  istio-ingressgateway:
    rollingMaxSurge: 4
    rollingMaxUnavailable: 1
    resources:
      requests:
        cpu: 1000m
        memory: 1G
    enabled: true
`,
			want: `
hub: docker.io/istio
tag: 1.2.3
meshConfig: 
  rootNamespace: istio-system
components:
  telemetry:
    enabled: true
  policy:
    enabled: true
  pilot:
    enabled: true
  ingressGateways:
  - name: istio-ingressgateway
    enabled: true
    k8s:
      resources:
        requests:
          cpu: 1000m
          memory: 1G
      strategy:
        rollingUpdate:
          maxSurge: 4
          maxUnavailable: 1
addonComponents:
   istiocoredns:
      enabled: true
values:
  global:
    policyNamespace: istio-policy
    telemetryNamespace: istio-telemetry
`,
		},
		{
			desc: "Some components Disabled",
			valueYAML: `
pilot:
  enabled: true
global:
  hub: docker.io/istio
  istioNamespace: istio-system
  policyNamespace: istio-policy
  tag: 1.2.3
  telemetryNamespace: istio-telemetry
mixer:
  policy:
    enabled: true
  telemetry:
    enabled: false
`,
			want: `
hub: docker.io/istio
tag: 1.2.3
components:
   telemetry:
     enabled: false
   policy:
     enabled: true
   pilot:
     enabled: true
meshConfig:
  rootNamespace: istio-system
values:
  global:
    telemetryNamespace: istio-telemetry
    policyNamespace: istio-policy
`,
		},
	}
	tr, err := NewReverseTranslator(version.NewMinorVersion(1, 5))
	if err != nil {
		t.Fatalf("fail to get helm value.yaml translator: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			valueStruct := v1alpha1.Values{}
			err = util.UnmarshalValuesWithJSONPB(tt.valueYAML, &valueStruct, false)
			if err != nil {
				t.Fatalf("unmarshal(%s): got error %s", tt.desc, err)
			}
			scope.Debugf("value struct: \n%s\n", pretty.Sprint(valueStruct))
			gotSpec, err := tr.TranslateFromValueToSpec([]byte(tt.valueYAML), false)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Errorf("ValuesToProto(%s)(%v): gotErr:%s, wantErr:%s", tt.desc, tt.valueYAML, gotErr, wantErr)
			}
			if tt.wantErr == "" {
				ms := jsonpb.Marshaler{}
				gotString, err := ms.MarshalToString(gotSpec)
				if err != nil {
					t.Errorf("failed to marshal translated IstioOperatorSpec: %s", err)
				}
				cpYaml, _ := yaml.JSONToYAML([]byte(gotString))
				if want := tt.want; !util.IsYAMLEqual(gotString, want) {
					t.Errorf("ValuesToProto(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, string(cpYaml), want, util.YAMLDiff(gotString, want))
				}

			}
		})
	}
}

func TestNewReverseTranslator(t *testing.T) {
	tests := []struct {
		name         string
		minorVersion version.MinorVersion
		wantVer      string
		wantErr      bool
	}{
		{
			name:         "version 1.4",
			minorVersion: version.NewMinorVersion(1, 4),
			wantVer:      "1.4",
			wantErr:      false,
		},
		// TODO: implement 1.5 and fallback logic.
		{
			name:         "version 1.99",
			minorVersion: version.NewMinorVersion(1, 99),
			wantVer:      "",
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewReverseTranslator(tt.minorVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewReverseTranslator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != nil && tt.wantVer != got.Version.String() {
				t.Errorf("NewReverseTranslator() got = %v, want %v", got.Version.String(), tt.wantVer)
			}
		})
	}
}
