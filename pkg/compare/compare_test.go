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

package compare

import (
	"testing"
)

func TestYAMLCmp(t *testing.T) {
	tests := []struct {
		desc string
		a    string
		b    string
		want interface{}
	}{
		{
			desc: "two additional",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  namespace: istio-system
  labels:
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `metadata:
  labels:
    app: -> istio-ingressgateway
  name: -> istio-ingressgateway
`,
		},
		{
			desc: "two missing",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  namespace: istio-system
  labels:
    release: istio`,
			want: `metadata:
  labels:
    app: istio-ingressgateway ->
  name: istio-ingressgateway ->
`,
		},
		{
			desc: "one missing",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    release: istio`,
			want: `metadata:
  labels:
    app: istio-ingressgateway ->
`,
		},
		{
			desc: "one additional",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `metadata:
  labels:
    app: -> istio-ingressgateway
`,
		},
		{
			desc: "identical",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			b: `apiVersion: autoscaling/v2beta2
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			want: `apiVersion: autoscaling/v2beta1 -> autoscaling/v2beta2
`,
		},
		{
			desc: "nested item changed",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
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
        targetAverageUtilization: 80`,
			b: `apiVersion: autoscaling/v2beta1
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
      targetAverageUtilization: 80
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
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2beta1
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
    '[1]': label2 -> label5
    '[2]': label3 -> label6
`,
		},
		{
			desc: "arrays with same items, order changed",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2beta1
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
    '[?->2]': -> label1
    '[0->?]': label1 ->
`,
		},
		{
			desc: "arrays with items",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    - label0
    - label1
    - label2
`,
			b: `apiVersion: autoscaling/v2beta1
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
    '[?->1]': -> label5
    '[?->2]': -> label2
    '[?->3]': -> label3
    '[0]': label0 -> label4
    '[2->5]': label2 -> label0
`,
		},
		{
			desc: "arrays with additional items",
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
 name: istio-ingressgateway
 namespace: istio-system
 labels:
   - label1
   - label2
   - label3
`,
			b: `apiVersion: autoscaling/v2beta1
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
    '[?->3]': -> label4
    '[?->4]': -> label5
`,
		},
		{
			desc: "arrays with missing items",
			a: `apiVersion: autoscaling/v2beta1
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
			b: `apiVersion: autoscaling/v2beta1
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
    '[3->?]': label4 ->
    '[4->?]': label5 ->
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
			a: `apiVersion: autoscaling/v2beta1
kind: HorizontalPodAutoscaler
metadata:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume:
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: istio-ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
    -> <nil>
`,
		},
		{
			desc: "ignore by wildcard",
			a: `apiVersion: autoscaling/v2beta1
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
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
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
			b: `apiVersion: autoscaling/v2beta1
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
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
				"metadata.labels.app"},
			want: ``,
		},
		{
			desc: "ignore multiple paths negative",
			a: `apiVersion: autoscaling/v2beta1
kind: Deployment
metadata:
  annotations:
    checksum/config-volume: 43d72e930ed33e3e01731f8bcbf31dbf02cb1c1fc53bcc09199ab45c0d031b60
  name: istio-ingressgateway
  namespace: istio-system
  labels:
    app: ingressgateway
    release: istio`,
			b: `apiVersion: autoscaling/v2beta1
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
				"metadata.annotations.checksum/config-volume"},
			want: `metadata:
  labels:
    app: ingressgateway -> istio-ingressgateway
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := YAMLCmpWithIgnore(tt.a, tt.b, tt.i), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}
