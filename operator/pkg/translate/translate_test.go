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
	"fmt"
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test/util/assert"
)

func Test_skipReplicaCountWithAutoscaleEnabled(t *testing.T) {
	const valuesWithHPAndReplicaCountFormat = `
values:
  pilot:
    autoscaleEnabled: %t
  gateways:
    istio-ingressgateway:
      autoscaleEnabled: %t
    istio-egressgateway:
      autoscaleEnabled: %t
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
`

	cases := []struct {
		name       string
		component  name.ComponentName
		values     string
		expectSkip bool
	}{
		{
			name:       "hpa enabled for pilot without replicas",
			component:  name.PilotComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, false, false, false),
			expectSkip: false,
		},
		{
			name:       "hpa enabled for ingressgateway without replica",
			component:  name.IngressComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, false, false, false),
			expectSkip: false,
		},
		{
			name:       "hpa enabled for pilot without replicas",
			component:  name.EgressComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, false, false, false),
			expectSkip: false,
		},
		{
			name:       "hpa enabled for pilot with replicas",
			component:  name.PilotComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, true, false, false),
			expectSkip: true,
		},
		{
			name:       "hpa enabled for ingressgateway with replicas",
			component:  name.IngressComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, false, true, false),
			expectSkip: true,
		},
		{
			name:       "hpa enabled for egressgateway with replicas",
			component:  name.EgressComponentName,
			values:     fmt.Sprintf(valuesWithHPAndReplicaCountFormat, true, false, true),
			expectSkip: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var iop *v1alpha1.IstioOperatorSpec
			if tt.values != "" {
				iop = &v1alpha1.IstioOperatorSpec{}
				if err := util.UnmarshalWithJSONPB(tt.values, iop, true); err != nil {
					t.Fatal(err)
				}
			}

			got := skipReplicaCountWithAutoscaleEnabled(iop, tt.component)
			assert.Equal(t, tt.expectSkip, got)
		})
	}
}

func Test_fixMergedObjectWithCustomServicePortOverlay_withAppProtocol(t *testing.T) {
	const serviceString = `
apiVersion: v1
kind: Service
metadata:
  name: istio-ingressgateway
  namespace: istio-system
spec:
  type: LoadBalancer
`
	const iopString = `
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        service:
          ports:
          - name: http2
            port: 80
            targetPort: 8080
`
	const iopString2 = `
components:
  ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        service:
          ports:
          - name: http2
            port: 80
            targetPort: 8080
            appProtocol: HTTP
`
	cases := []struct {
		name        string
		iopString   string
		expectValue string
		expectExist bool
	}{
		{
			name:        "without appProtocol",
			iopString:   iopString,
			expectExist: false,
		},
		{
			name:        "with appProtocol",
			iopString:   iopString2,
			expectExist: true,
			expectValue: "HTTP",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var iop *v1alpha1.IstioOperatorSpec
			if tt.iopString != "" {
				iop = &v1alpha1.IstioOperatorSpec{}
				if err := util.UnmarshalWithJSONPB(tt.iopString, iop, true); err != nil {
					t.Fatal(err)
				}
			}
			translator := NewTranslator()
			serviceObj, err := object.ParseYAMLToK8sObject([]byte(serviceString))
			assert.NoError(t, err)
			obj, err := translator.fixMergedObjectWithCustomServicePortOverlay(serviceObj,
				iop.Components.IngressGateways[0].GetK8S().GetService(), serviceObj)
			assert.NoError(t, err)
			val := obj.UnstructuredObject().Object["spec"].(map[string]interface{})["ports"].([]interface{})[0]
			apVal, found, _ := tpath.GetFromStructPath(val, "appProtocol")
			if !tt.expectExist {
				assert.Equal(t, found, false)
			} else {
				if apVal != nil {
					assert.Equal(t, apVal.(string), tt.expectValue)
				} else {
					assert.Equal(t, "", tt.expectValue)
				}
			}
		})
	}
}
