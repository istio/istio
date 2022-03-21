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

	"github.com/gogo/protobuf/jsonpb"
	"github.com/kr/pretty"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/util"
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
    kubernetes.io/os: linux
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
global:
  hub: docker.io/istio
  istioNamespace: istio-system
  tag: 1.2.3
  proxy:
    readinessInitialDelaySeconds: 2
`,
			want: `
hub: docker.io/istio
tag: 1.2.3
components:
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
          kubernetes.io/os: linux
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
    istioNamespace: istio-system
    proxy:
      readinessInitialDelaySeconds: 2
  pilot:
    image: pilot
    autoscaleEnabled: true
    traceSampling: 1
`,
		},
		{
			desc: "All Enabled",
			valueYAML: `
global:
  hub: docker.io/istio
  istioNamespace: istio-system
  tag: 1.2.3
pilot:
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
components:
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
values:
  global:
    istioNamespace: istio-system
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
  tag: 1.2.3
`,
			want: `
hub: docker.io/istio
tag: 1.2.3
components:
   pilot:
     enabled: true
values:
  global:
    istioNamespace: istio-system
`,
		},
	}
	tr := NewReverseTranslator()

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			valueStruct := v1alpha1.Values{}
			err := util.UnmarshalWithJSONPB(tt.valueYAML, &valueStruct, false)
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

func TestValueToK8s(t *testing.T) {
	tests := []struct {
		desc      string
		inIOPSpec string
		want      string
		wantErr   string
	}{
		{
			desc: "pilot env k8s setting with values",
			inIOPSpec: `
spec:
  components:
    pilot:
      k8s:
        nodeSelector:
          master: "true"
        env:
        - name: EXTERNAL_CA
          value: ISTIOD_RA_KUBERNETES_API
        - name: K8S_SIGNER
          value: kubernetes.io/legacy-unknown
  values:
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
        kubernetes.io/os: linux
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
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
`,
			want: `
spec:
  components:
    pilot:
      k8s:
        env:
        - name: EXTERNAL_CA
          value: ISTIOD_RA_KUBERNETES_API
        - name: K8S_SIGNER
          value: kubernetes.io/legacy-unknown
        - name: GODEBUG
          value: gctrace=1
        hpaSpec:
          minReplicas: 1
          maxReplicas: 3
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
        nodeSelector:
          master: "true"
          kubernetes.io/os: linux
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
        strategy:
          rollingUpdate:
            maxSurge: 100%
            maxUnavailable: 25%
        tolerations:
        - effect: NoSchedule
          key: dedicated
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
  values:
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
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
        kubernetes.io/os: linux
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
`,
		},
		{
			desc: "pilot no env k8s setting with values",
			inIOPSpec: `
spec:
  components:
    pilot:
      k8s:
        nodeSelector:
          master: "true"
  values:
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
        kubernetes.io/os: linux
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
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
`,
			want: `
spec:
  components:
    pilot:
      k8s:
        env:
        - name: GODEBUG
          value: gctrace=1
        hpaSpec:
          minReplicas: 1
          maxReplicas: 3
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
        nodeSelector:
          master: "true"
          kubernetes.io/os: linux
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
        strategy:
          rollingUpdate:
            maxSurge: 100%
            maxUnavailable: 25%
        tolerations:
        - effect: NoSchedule
          key: dedicated
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
  values:
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
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
        kubernetes.io/os: linux
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
`,
		},
		{
			desc: "pilot k8s setting with empty env in values",
			inIOPSpec: `
spec:
  components:
    pilot:
      k8s:
        nodeSelector:
          master: "true"
        env:
        - name: SPIFFE_BUNDLE_ENDPOINTS
          value: "SPIFFE_BUNDLE_ENDPOINT"
  values:
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
        kubernetes.io/os: linux
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
      env: {}
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
`,
			want: `
spec:
  components:
    pilot:
      k8s:
        env:
        - name: SPIFFE_BUNDLE_ENDPOINTS
          value: "SPIFFE_BUNDLE_ENDPOINT"
        hpaSpec:
          minReplicas: 1
          maxReplicas: 3
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
        nodeSelector:
          master: "true"
          kubernetes.io/os: linux
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
        strategy:
          rollingUpdate:
            maxSurge: 100%
            maxUnavailable: 25%
        tolerations:
        - effect: NoSchedule
          key: dedicated
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
  values:
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
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
        kubernetes.io/os: linux
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
      env: {}
`,
		},
		{
			desc: "pilot no env k8s setting with multiple env variables in values",
			inIOPSpec: `
spec:
  components:
    pilot:
      k8s:
        nodeSelector:
          master: "true"
  values:
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
        kubernetes.io/os: linux
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
        PILOT_ENABLE_ISTIO_TAGS: false
        PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME: false
        PROXY_XDS_DEBUG_VIA_AGENT: false
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
`,
			want: `
spec:
  components:
    pilot:
      k8s:
        env:
        - name: PILOT_ENABLE_ISTIO_TAGS
          value: "false"
        - name: PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME
          value: "false"
        - name: PROXY_XDS_DEBUG_VIA_AGENT
          value: "false"
        hpaSpec:
          minReplicas: 1
          maxReplicas: 3
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
        nodeSelector:
          master: "true"
          kubernetes.io/os: linux
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
        strategy:
          rollingUpdate:
            maxSurge: 100%
            maxUnavailable: 25%
        tolerations:
        - effect: NoSchedule
          key: dedicated
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
  values:
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
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
        kubernetes.io/os: linux
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
        PILOT_ENABLE_ISTIO_TAGS: false
        PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME: false
        PROXY_XDS_DEBUG_VIA_AGENT: false
`,
		},
		{
			desc: "pilot k8s setting with multiple env variables in values",
			inIOPSpec: `
spec:
  components:
    pilot:
      k8s:
        nodeSelector:
          master: "true"
        env:
        - name: SPIFFE_BUNDLE_ENDPOINTS
          value: "SPIFFE_BUNDLE_ENDPOINT"
  values:
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
        kubernetes.io/os: linux
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
        PILOT_ENABLE_ISTIO_TAGS: false
        PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME: false
        PROXY_XDS_DEBUG_VIA_AGENT: false
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
`,
			want: `
spec:
  components:
    pilot:
      k8s:
        env:
        - name: SPIFFE_BUNDLE_ENDPOINTS
          value: "SPIFFE_BUNDLE_ENDPOINT"
        - name: PILOT_ENABLE_ISTIO_TAGS
          value: "false"
        - name: PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME
          value: "false"
        - name: PROXY_XDS_DEBUG_VIA_AGENT
          value: "false"
        hpaSpec:
          minReplicas: 1
          maxReplicas: 3
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
        nodeSelector:
          master: "true"
          kubernetes.io/os: linux
        replicaCount: 1
        resources:
          requests:
            cpu: 1000m
            memory: 1G
        strategy:
          rollingUpdate:
            maxSurge: 100%
            maxUnavailable: 25%
        tolerations:
        - effect: NoSchedule
          key: dedicated
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
  values:
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
      proxy:
        readinessInitialDelaySeconds: 2
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
        kubernetes.io/os: linux
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
        PILOT_ENABLE_ISTIO_TAGS: false
        PILOT_ENABLE_LEGACY_ISTIO_MUTUAL_CREDENTIAL_NAME: false
        PROXY_XDS_DEBUG_VIA_AGENT: false
`,
		},
		{
			desc: "ingressgateway k8s setting with values",
			inIOPSpec: `
spec:
  components:
    pilot:
      enabled: false
    ingressGateways:
    - namespace: istio-system
      name: istio-ingressgateway
      enabled: true
      k8s:
        service:
          externalTrafficPolicy: Local
        serviceAnnotations:
          manifest-generate: "testserviceAnnotation"
        securityContext:
          sysctls:
          - name: "net.ipv4.ip_local_port_range"
            value: "80 65535"
  values:
    gateways:
      istio-ingressgateway:
        serviceAnnotations: {}
        nodeSelector: {}
        tolerations: []
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
`,
			want: `
spec:
  components:
    ingressGateways:
    - name: istio-ingressgateway
      namespace: istio-system
      enabled: true
      k8s:
        securityContext:
          sysctls:
          - name: net.ipv4.ip_local_port_range
            value: "80 65535"
        service:
          externalTrafficPolicy: Local
        serviceAnnotations:
          manifest-generate: testserviceAnnotation
    pilot:
      enabled: false
  values:
    gateways:
      istio-ingressgateway:
        serviceAnnotations: {}
        nodeSelector: {}
        tolerations: []
    global:
      hub: docker.io/istio
      tag: 1.2.3
      istioNamespace: istio-system
`,
		},
		{
			desc: "pilot env k8s setting with non-empty hpa values",
			inIOPSpec: `
spec:
  revision: canary
  components:
    pilot:
      enabled: true
  values:
    pilot:
      autoscaleMin: 1
      autoscaleMax: 3
      cpu:
        targetAverageUtilization: 80
`,
			want: `
spec:
  revision: canary
  components:
    pilot:
      enabled: true
      k8s:
        hpaSpec:
          maxReplicas: 3
          minReplicas: 1
          scaleTargetRef:
            apiVersion: apps/v1
            kind: Deployment
            name: istiod-canary
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageUtilization: 80
  values:
    pilot:
      autoscaleMin: 1
      autoscaleMax: 3
      cpu:
        targetAverageUtilization: 80
`,
		},
	}
	tr := NewReverseTranslator()

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			iopStruct := v1alpha1.IstioOperator{}
			err := util.UnmarshalWithJSONPB(tt.inIOPSpec, &iopStruct, false)
			if err != nil {
				t.Fatalf("unmarshal(%s): got error %s", tt.desc, err)
			}
			scope.Debugf("value struct: \n%s\n", pretty.Sprint(iopStruct))
			gotSpec, err := tr.TranslateK8SfromValueToIOP(tt.inIOPSpec)
			if gotErr, wantErr := errToString(err), tt.wantErr; gotErr != wantErr {
				t.Errorf("ValuesToK8s(%s)(%v): gotErr:%s, wantErr:%s", tt.desc, tt.inIOPSpec, gotErr, wantErr)
			}
			if tt.wantErr == "" {
				if want := tt.want; !util.IsYAMLEqual(gotSpec, want) {
					t.Errorf("ValuesToK8s(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, gotSpec, want, util.YAMLDiff(gotSpec, want))
				}
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
