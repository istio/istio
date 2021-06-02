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

package mesh

import (
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/object"
)

func TestManifestFromString(t *testing.T) {
	tests := []struct {
		desc         string
		given        string
		mustContains []string
		expectErr    bool
	}{
		{
			desc: "with enabled component",
			given: `---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
spec:
  components:
    ingressGateways:
    - name: istio-ingressgateway
      enabled: true`,
			mustContains: []string{
				"name: istio-ingressgateway-service-account",
				"name: istio-ingressgateway",
			},
			expectErr: false,
		},
		{
			desc: "with no enabled component",
			given: `---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
spec:
  components:
    pilot:
      k8s:
        podDisruptionBudget:
          maxUnavailable: 2`,
			mustContains: []string{""},
			expectErr:    false,
		},
		{
			desc: "invalid spec",
			given: `---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: invalid-virtual-service
spec:
  http`,
			mustContains: []string{""},
			expectErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := ManifestFromString(tt.given)
			if !tt.expectErr {
				if err != nil {
					t.Errorf("Expect no errors but got one %v", err)
				} else {
					for _, mcs := range tt.mustContains {
						if !strings.Contains(got, mcs) {
							t.Errorf("Results must contain %s, but is not found in the results", mcs)
						}
					}
				}
			} else if err == nil {
				t.Errorf("Expected error but did not get any")
			}
		})
	}
}

func TestShowDifferences(t *testing.T) {
	getK8sObj := func(content string) *object.K8sObject {
		obj, err := object.ParseYAMLToK8sObject([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		return obj
	}
	testInput1 := `---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-operator-test
  name: test-operator
spec:
  values:
    global:
      logging:
        level: "default:info"
  components:
    ingressGateways:
    - name: istio-ingressgateway
      enabled: true`
	testInput2 := `---
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-operator-test
  name: test-operator
spec:
  values:
    global:
      logging:
        level: "default:error"
  components:
    ingressGateways:
    - name: istio-ingressgateway
      enabled: true`
	tests := []struct {
		name         string
		diff1        string
		diff2        string
		mustContains []string
	}{
		{
			name:  "different inputs",
			diff1: testInput1,
			diff2: testInput2,
			mustContains: []string{
				"-        level: default:info",
				"+        level: default:error",
			},
		},
		{
			name:         "same inputs",
			diff1:        testInput1,
			diff2:        testInput1,
			mustContains: []string{""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := showDifferences(getK8sObj(tt.diff1).UnstructuredObject(),
				getK8sObj(tt.diff2).UnstructuredObject())
			if err != nil {
				t.Errorf("Expected no error but got error %v", err)
			} else {
				for _, mcs := range tt.mustContains {
					if !strings.Contains(result, mcs) {
						t.Errorf("Results must contain %s, but is not found in the results", mcs)
					}
				}
			}
		})
	}
}
