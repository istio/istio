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

package compare

import (
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/object"
)

func TestYAMLCmp(t *testing.T) {
	tests := []struct {
		desc string
		a    string
		b    string
		want interface{}
	}{
		{
			desc: "empty string into nil",
			a:    `metadata: ""`,
			b:    `metadata: `,
			want: ``,
		},
		{
			desc: "empty array into nil",
			a:    `metadata: []`,
			b:    `metadata: `,
			want: ``,
		},
		{
			desc: "empty map into nil",
			a:    `metadata: {}`,
			b:    `metadata: `,
			want: ``,
		},
		{
			desc: "two additional",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  namespace: istio-system
  labels:
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `metadata:
  labels:
    app: <empty> -> istio-ingressgateway (ADDED)
  name: <empty> -> istio-ingressgateway (ADDED)
`,
		},
		{
			desc: "two missing",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  namespace: istio-system
  labels:
    release: istio`,
			want: `metadata:
  labels:
    app: istio-ingressgateway -> <empty> (REMOVED)
  name: istio-ingressgateway -> <empty> (REMOVED)
`,
		},
		{
			desc: "one missing",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    release: istio`,
			want: `metadata:
  labels:
    app: istio-ingressgateway -> <empty> (REMOVED)
`,
		},
		{
			desc: "one additional",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `metadata:
  labels:
    app: <empty> -> istio-ingressgateway (ADDED)
`,
		},
		{
			desc: "identical",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: ``,
		},
		{
			desc: "first item changed",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `apiVersion: autoscaling/v2beta1 -> autoscaling/v2
`,
		},
		{
			desc: "nested item changed",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-egressgateway
    release: istio`,
			want: `metadata:
  labels:
    app: istio-ingressgateway -> istio-egressgateway
`,
		},
		{
			desc: "one map value changed, order changed",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio
spec:
  maxReplicas: 5
  minReplicas: 1
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ingressgateway
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  labels:
    app: istio-ingressgateway
    release: istio
  name: istio-ingressgateway
  namespace: istio-system
spec:
  maxReplicas: 5
  metrics:
  - resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 80
    type: Resource
  minReplicas: 1
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: istio-ingressgateway`,
			want: `spec:
  scaleTargetRef:
    name: ingressgateway -> istio-ingressgateway
`,
		},
		{
			desc: "arrays with same items",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			want: ``,
		},
		{
			desc: "arrays with different items",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label5
   - label6
`,
			want: `metadata:
  labels:
    '[#1]': label2 -> label5
    '[#2]': label3 -> label6
`,
		},
		{
			desc: "arrays with same items, order changed",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label2
   - label3
   - label1
`,
			want: `metadata:
  labels:
    '[?->2]': <empty> -> label1 (ADDED)
    '[0->?]': label1 -> <empty> (REMOVED)
`,
		},
		{
			desc: "arrays with items",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    - label0
    - label1
    - label2
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    - label4
    - label5
    - label2
    - label3
    - label1
    - label0
`,
			want: `metadata:
  labels:
    '[#0]': label0 -> label4
    '[?->1]': <empty> -> label5 (ADDED)
    '[?->2]': <empty> -> label2 (ADDED)
    '[?->3]': <empty> -> label3 (ADDED)
    '[2->5]': label2 -> label0
`,
		},
		{
			desc: "arrays with additional items",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
   - label4
   - label5
`,
			want: `metadata:
  labels:
    '[?->3]': <empty> -> label4 (ADDED)
    '[?->4]': <empty> -> label5 (ADDED)
`,
		},
		{
			desc: "arrays with missing items",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
   - label4
   - label5
`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			want: `metadata:
  labels:
    '[3->?]': label4 -> <empty> (REMOVED)
    '[4->?]': label5 -> <empty> (REMOVED)
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := YAMLCmp(tt.a, tt.b), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestYAMLCmpWithIgnore(t *testing.T) {
	tests := []struct {
		desc string
		a    string
		b    string
		i    []string
		want interface{}
	}{
		{
			desc: "identical",
			a: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i:    []string{"metadata.annotations.checksum/config-volume"},
			want: ``,
		},
		{
			desc: "ignore checksum",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i:    []string{"metadata.annotations.checksum/config-volume"},
			want: ``,
		},
		{
			desc: "ignore missing checksum value",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i:    []string{"metadata.annotations.checksum/config-volume"},
			want: ``,
		},
		{
			desc: "ignore additional checksum value",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i:    []string{"metadata.annotations.checksum/config-volume"},
			want: ``,
		},
		{
			desc: "show checksum not exist",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i: []string{"metadata.annotations.checksum/config-volume"},
			want: `metadata:
  annotations: map[checksum/config-volume:43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60]
    -> <empty> (REMOVED)
`,
		},
		{
			desc: "ignore by wildcard",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 01d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    checksum/config-volume: 02ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    checksum/config-volume: 04ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
    app: istio-ingressgateway
    release: istio`,
			i:    []string{"*.checksum/config-volume"},
			want: ``,
		},
		{
			desc: "ignore by wildcard negative",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 01d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    checksum/config-volume: 02ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    checksum/config-volume: 04ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
    app: istio-ingressgateway
    release: istio`,
			i: []string{"*labels.checksum/config-volume"},
			want: `metadata:
  annotations:
    checksum/config-volume: 01d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
      -> 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
`,
		},
		{
			desc: "ignore multiple paths",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i: []string{
				"metadata.annotations.checksum/config-volume",
				"metadata.labels.app",
			},
			want: ``,
		},
		{
			desc: "ignore multiple paths negative",
			a: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 03ba6246b2c39b48a4f8c3a92c3420a0416804d38ebe292e65cf674fb0875192
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			i: []string{
				"metadata.annotations.checksum/config-volume",
			},
			want: `metadata:
  labels:
    app: ingressgateway -> istio-ingressgateway
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := YAMLCmpWithIgnore(tt.a, tt.b, tt.i, ""), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestYAMLCmpWithIgnoreTree(t *testing.T) {
	tests := []struct {
		desc string
		a    string
		b    string
		mask string
		want interface{}
	}{
		{
			desc: "ignore masked",
			a: `
ak: av
bk:
  b1k: b1v
  b2k: b2v
`,
			b: `
ak: av
bk:
  b1k: b1v-changed
  b2k: b2v-changed
`,
			mask: `
bk:
  b1k: ignored
  b2k: ignored
`,
			want: ``,
		},
		{
			desc: "ignore nested masked",
			a: `
ak: av
bk:
  bbk:
    b1k: b1v
    b2k: b2v
`,
			b: `
ak: av
bk:
  bbk:
    b1k: b1v-changed
    b2k: b2v-changed
`,
			mask: `
bk:
  bbk:
    b1k: ignored
`,
			want: `bk:
  bbk:
    b2k: b2v -> b2v-changed
`,
		},
		{
			desc: "not ignore non-masked",
			a: `
ak: av
bk:
  bbk:
    b1k: b1v
    b2k: b2v
`,
			b: `
ak: av
bk:
  bbk:
    b1k: b1v-changed
    b2k: b2v
`,
			mask: `
bk:
  bbk:
    b1k:
      bbb1k: ignored
`,
			want: `bk:
  bbk:
    b1k: b1v -> b1v-changed
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := YAMLCmpWithIgnore(tt.a, tt.b, nil, tt.mask), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestYAMLCmpWithYamlInline(t *testing.T) {
	tests := []struct {
		desc string
		a    string
		b    string
		want interface{}
	}{
		{
			desc: "ConfigMap data order changed",
			a: `kind: ConfigMap
data:
  validatingwebhookconfiguration.yaml: |-
    kind: ValidatingWebhookConfiguration
    apiVersion: admissionregistration.k8s.io/v1beta1
    metadata:
      name: istio-galley
      value: foo`,
			b: `kind: ConfigMap
data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      value: foo
      name: istio-galley`,
			want: ``,
		},
		{
			desc: "ConfigMap data value change",
			a: `kind: ConfigMap
data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: istio-galley`,
			b: `kind: ConfigMap
data:
  validatingwebhookconfiguration.yaml: |-
    kind: ValidatingWebhookConfiguration
    apiVersion: admissionregistration.k8s.io/v1beta2
    metadata:
      name: istio-galley`,
			want: `data:
  validatingwebhookconfiguration.yaml:
    apiVersion: admissionregistration.k8s.io/v1beta1 -> admissionregistration.k8s.io/v1beta2
`,
		},
		{
			desc: "ConfigMap data deep nested value change",
			a: `apiVersion: v1
kind: ConfigMap
metadata:
  name: injector-mesh
data:
  mesh: |-
    defaultConfig:
      tracing:
        zipkin:
          address: zipkin.istio-system:9411
      controlPlaneAuthPolicy: NONE
      connectTimeout: 10s`,
			b: `apiVersion: v1
kind: ConfigMap
metadata:
  name: injector-mesh
data:
  mesh: |-
    defaultConfig:
      connectTimeout: 10s
      tracing:
        zipkin:
          address: zipkin.istio-system:9412
      controlPlaneAuthPolicy: NONE`,
			want: `data:
  mesh:
    defaultConfig:
      tracing:
        zipkin:
          address: zipkin.istio-system:9411 -> zipkin.istio-system:9412
`,
		},
		{
			desc: "ConfigMap data multiple changes",
			a: `apiVersion: v1
kind: ConfigMap
metadata:
  name: injector-mesh
  namespace: istio-system
  labels:
    release: istio
data:
  mesh: |-
    defaultConfig:
      connectTimeout: 10s
      configPath: "/etc/istio/proxyv1"
      serviceCluster: istio-proxy
      drainDuration: 45s
      parentShutdownDuration: 1m0s
      proxyAdminPortA: 15000
      concurrency: 2
      tracing:
        zipkin:
          address: zipkin.istio-system:9411
      controlPlaneAuthPolicy: NONE
      discoveryAddress: istio-pilot.istio-system:15010`,
			b: `apiVersion: v1
kind: ConfigMap
metadata:
  name: injector-mesh
  namespace: istio-system
  labels:
    release: istio
data:
  mesh: |-
    defaultConfig:
      connectTimeout: 10s
      configPath: "/etc/istio/proxyv2"
      serviceCluster: istio-proxy
      drainDuration: 45s
      parentShutdownDuration: 1m0s
      proxyAdminPortB: 15000
      concurrency: 2
      tracing:
        zipkin:
          address: zipkin.istio-system:9411
      controlPlaneAuthPolicy: NONE
      discoveryAddress: istio-pilot.istio-system:15010`,
			want: `data:
  mesh:
    defaultConfig:
      configPath: /etc/istio/proxyv1 -> /etc/istio/proxyv2
      proxyAdminPortA: 15000 -> <empty> (REMOVED)
      proxyAdminPortB: <empty> -> 15000 (ADDED)
`,
		},
		{
			desc: "Not ConfigMap, identical",
			a: `kind: Config
data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: istio-galley`,
			b: `kind: Config
data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: istio-galley`,
			want: ``,
		},
		{
			desc: "Not ConfigMap, Order changed",
			a: `kind: Config
data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: istio-galley`,
			b: `kind: Config
data:
  validatingwebhookconfiguration.yaml: |-
    kind: ValidatingWebhookConfiguration
    apiVersion: admissionregistration.k8s.io/v1beta1
    metadata:
      name: istio-galley`,
			want: `data:
  validatingwebhookconfiguration.yaml: |-
    apiVersion: admissionregistration.k8s.io/v1beta1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: istio-galley -> kind: ValidatingWebhookConfiguration
    apiVersion: admissionregistration.k8s.io/v1beta1
    metadata:
      name: istio-galley
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := YAMLCmp(tt.a, tt.b), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}

func TestManifestDiff(t *testing.T) {
	testDeploymentYaml1 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
`

	testDeploymentYaml2 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.2.2
`

	testPodYaml1 := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP
`

	testServiceYaml1 := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
`

	manifestDiffTests := []struct {
		desc        string
		yamlStringA string
		yamlStringB string
		want        string
	}{
		{
			"ManifestDiffWithIdenticalResource",
			testDeploymentYaml1 + object.YAMLSeparator,
			testDeploymentYaml1,
			"",
		},
		{
			"ManifestDiffWithIdenticalMultipleResources",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testPodYaml1 + object.YAMLSeparator + testServiceYaml1 + object.YAMLSeparator + testDeploymentYaml1,
			"",
		},
		{
			"ManifestDiffWithDifferentResource",
			testDeploymentYaml1,
			testDeploymentYaml2,
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithDifferentMultipleResources",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testDeploymentYaml2 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffMissingResourcesInA",
			testPodYaml1 + object.YAMLSeparator + testDeploymentYaml1 + object.YAMLSeparator,
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			"Object Service:istio-system:istio-pilot is missing in A",
		},
		{
			"ManifestDiffMissingResourcesInB",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + object.YAMLSeparator + testPodYaml1,
			"Object Deployment:istio-system:istio-citadel is missing in B",
		},
	}

	for _, tt := range manifestDiffTests {
		for _, v := range []bool{true, false} {
			t.Run(tt.desc, func(t *testing.T) {
				got, err := ManifestDiff(tt.yamlStringA, tt.yamlStringB, v)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("%s:\ngot:\n%v\ndoes't contains\nwant:\n%v", tt.desc, got, tt.want)
				}
			})
		}
	}
}

func TestManifestDiffWithSelectAndIgnore(t *testing.T) {
	testDeploymentYaml1 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testDeploymentYaml2 := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.2.2
---
`

	testPodYaml1 := `apiVersion: v1
kind: Pod
metadata:
  name: istio-galley-75bcd59768-hpt5t
  namespace: istio-system
  labels:
    istio: galley
spec:
  containers:
  - name: galley
    image: docker.io/istio/galley:1.1.8
    ports:
    - containerPort: 443
      protocol: TCP
    - containerPort: 15014
      protocol: TCP
    - containerPort: 9901
      protocol: TCP
---
`

	testServiceYaml1 := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	manifestDiffWithSelectAndIgnoreTests := []struct {
		desc            string
		yamlStringA     string
		yamlStringB     string
		selectResources string
		ignoreResources string
		want            string
	}{
		{
			"ManifestDiffWithSelectAndIgnoreForIdenticalResource",
			testDeploymentYaml1 + object.YAMLSeparator,
			testDeploymentYaml1,
			"::",
			"",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesIgnoreSingle",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testDeploymentYaml1 + object.YAMLSeparator,
			"Deployment:*:istio-citadel",
			"Service:*:",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesIgnoreMultiple",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testDeploymentYaml1,
			"Deployment:*:istio-citadel",
			"Pod::*,Service:istio-system:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesSelectSingle",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + object.YAMLSeparator + testDeploymentYaml1,
			"Deployment::istio-citadel",
			"Pod:*:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourcesSelectSingle",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + object.YAMLSeparator + testDeploymentYaml1,
			"Deployment::istio-citadel,Service:istio-system:istio-pilot,Pod:*:*",
			"Pod:*:*",
			"",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourceForDefault",
			testDeploymentYaml1,
			testDeploymentYaml2 + object.YAMLSeparator,
			"::",
			"",
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForDifferentResourceForSingleSelectAndIgnore",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testDeploymentYaml2 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			"Deployment:*:*",
			"Pod:*:*",
			"Object Deployment:istio-system:istio-citadel has diffs",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForMissingResourcesInA",
			testPodYaml1 + object.YAMLSeparator + testDeploymentYaml1,
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			"Pod:istio-system:Citadel,Service:istio-system:",
			"Pod:*:*",
			"Object Service:istio-system:istio-pilot is missing in A",
		},
		{
			"ManifestDiffWithSelectAndIgnoreForMissingResourcesInB",
			testDeploymentYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator + testServiceYaml1,
			testServiceYaml1 + object.YAMLSeparator + testPodYaml1 + object.YAMLSeparator,
			"*:istio-system:*",
			"Pod::",
			"Object Deployment:istio-system:istio-citadel is missing in B",
		},
	}

	for _, tt := range manifestDiffWithSelectAndIgnoreTests {
		for _, v := range []bool{true, false} {
			t.Run(tt.desc, func(t *testing.T) {
				got, err := ManifestDiffWithRenameSelectIgnore(tt.yamlStringA, tt.yamlStringB,
					"", tt.selectResources, tt.ignoreResources, v)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(got, tt.want) {
					t.Errorf("%s:\ngot:\n%v\ndoes't contains\nwant:\n%v", tt.desc, got, tt.want)
				}
			})
		}
	}
}

func TestManifestDiffWithRenameSelectIgnore(t *testing.T) {
	testDeploymentYaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-citadel
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testDeploymentYamlRenamed := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: istio-ca
  namespace: istio-system
  labels:
    istio: citadel
spec:
  replicas: 1
  selector:
    matchLabels:
      istio: citadel
  template:
    metadata:
      labels:
        istio: citadel
    spec:
      containers:
      - name: citadel
        image: docker.io/istio/citadel:1.1.8
---
`

	testServiceYaml := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-pilot
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	testServiceYamlRenamed := `apiVersion: v1
kind: Service
metadata:
  labels:
    app: pilot
  name: istio-control
  namespace: istio-system
spec:
  type: ClusterIP
  ports:
  - name: grpc-xds
    port: 15010
    protocol: TCP
    targetPort: 15010
  - name: http-monitoring
    port: 15014
    protocol: TCP
    targetPort: 15014
  selector:
    istio: pilot
---
`

	manifestDiffWithRenameSelectIgnoreTests := []struct {
		desc            string
		yamlStringA     string
		yamlStringB     string
		renameResources string
		selectResources string
		ignoreResources string
		want            string
	}{
		{
			"ManifestDiffDeployWithRenamedFlagMultiResourceWildcard",
			testDeploymentYaml + object.YAMLSeparator + testServiceYaml,
			testDeploymentYamlRenamed + object.YAMLSeparator + testServiceYamlRenamed,
			"Service:*:istio-pilot->::istio-control,Deployment::istio-citadel->::istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca


Object Service:istio-system:istio-control has diffs:

metadata:
  name: istio-pilot -> istio-control
`,
		},
		{
			"ManifestDiffDeployWithRenamedFlagMultiResource",
			testDeploymentYaml + object.YAMLSeparator + testServiceYaml,
			testDeploymentYamlRenamed + object.YAMLSeparator + testServiceYamlRenamed,
			"Service:istio-system:istio-pilot->Service:istio-system:istio-control,Deployment:istio-system:istio-citadel->Deployment:istio-system:istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca


Object Service:istio-system:istio-control has diffs:

metadata:
  name: istio-pilot -> istio-control
`,
		},
		{
			"ManifestDiffDeployWithRenamedFlag",
			testDeploymentYaml,
			testDeploymentYamlRenamed,
			"Deployment:istio-system:istio-citadel->Deployment:istio-system:istio-ca",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca has diffs:

metadata:
  name: istio-citadel -> istio-ca
`,
		},
		{
			"ManifestDiffRenamedDeploy",
			testDeploymentYaml,
			testDeploymentYamlRenamed,
			"",
			"::",
			"",
			`

Object Deployment:istio-system:istio-ca is missing in A:



Object Deployment:istio-system:istio-citadel is missing in B:

`,
		},
	}

	for _, tt := range manifestDiffWithRenameSelectIgnoreTests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := ManifestDiffWithRenameSelectIgnore(tt.yamlStringA, tt.yamlStringB,
				tt.renameResources, tt.selectResources, tt.ignoreResources, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("%s:\ngot:\n%v\ndoes't equals to\nwant:\n%v", tt.desc, got, tt.want)
			}
		})
	}
}
