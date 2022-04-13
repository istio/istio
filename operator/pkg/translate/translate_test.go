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

	"sigs.k8s.io/yaml"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/name"
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
			name:       "hpa enabled for ingressgateway with replicass",
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
				if err := util.UnmarshalWithJSONPB(tt.values, iop, false); err != nil {
					t.Fatal(err)
				}
			}

			got := skipReplicaCountWithAutoscaleEnabled(iop, tt.component)
			assert.Equal(t, tt.expectSkip, got)
		})
	}
}

func Test_sanitizeHelmValues(t *testing.T) {
	cases := []struct {
		name          string
		componentMaps *ComponentMaps
		rootYAML      string
		expectRoot    map[string]interface{}
	}{
		{
			name: "global",
			componentMaps: &ComponentMaps{
				ToHelmValuesTreeRoot: "global",
				HelmValuesType:       &iopv1alpha1.GlobalConfig{},
			},
			rootYAML: `
global:
  enabled: true
  namespace: istio-system
  hub: docker.io/istio
  tag: latest
pilot:
  enabled: true
`,
			expectRoot: map[string]interface{}{
				"global": map[string]interface{}{
					"hub": "docker.io/istio",
					"tag": "latest",
				},
				"pilot": map[string]interface{}{
					"enabled": true,
				},
			},
		},
		{
			name: "pilot",
			componentMaps: &ComponentMaps{
				ToHelmValuesTreeRoot: "pilot",
				HelmValuesType:       &iopv1alpha1.PilotConfig{},
			},
			rootYAML: `
pilot:
  enabled: true
  namespace: istio-system
  hub: docker.io/istio
  tag: latest
`,
			expectRoot: map[string]interface{}{
				"pilot": map[string]interface{}{
					"enabled": true,
					"hub":     "docker.io/istio",
					"tag":     "latest",
				},
			},
		},
		{
			name: "gateways.istio-ingressgateway",
			componentMaps: &ComponentMaps{
				ToHelmValuesTreeRoot: "gateways.istio-ingressgateway",
				HelmValuesType:       &iopv1alpha1.IngressGatewayConfig{},
			},
			rootYAML: `
gateways:
  istio-ingressgateway:
    enabled: true
    namespace: istio-system
    hub: docker.io/istio
    tag: latest
`,
			expectRoot: map[string]interface{}{
				"gateways": map[string]interface{}{
					"istio-ingressgateway": map[string]interface{}{
						"enabled":   true,
						"namespace": "istio-system",
						"hub":       "docker.io/istio",
						"tag":       "latest",
					},
				},
			},
		},
		{
			name: "gateways.istio-egressgateway",
			componentMaps: &ComponentMaps{
				ToHelmValuesTreeRoot: "gateways.istio-egressgateway",
				HelmValuesType:       &iopv1alpha1.EgressGatewayConfig{},
			},
			rootYAML: `
gateways:
  istio-egressgateway:
    enabled: true
    namespace: istio-system
    hub: docker.io/istio
    tag: latest
`,
			expectRoot: map[string]interface{}{
				"gateways": map[string]interface{}{
					"istio-egressgateway": map[string]interface{}{
						"enabled":   true,
						"namespace": "istio-system",
						"hub":       "docker.io/istio",
						"tag":       "latest",
					},
				},
			},
		},
		{
			name: "cni",
			componentMaps: &ComponentMaps{
				ToHelmValuesTreeRoot: "cni",
				HelmValuesType:       &iopv1alpha1.CNIConfig{},
			},
			rootYAML: `
cni:
  enabled: true
  namespace: istio-system
  hub: docker.io/istio
  tag: latest
`,
			expectRoot: map[string]interface{}{
				"cni": map[string]interface{}{
					"enabled": true,
					"hub":     "docker.io/istio",
					"tag":     "latest",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var root map[string]interface{}
			err := yaml.Unmarshal([]byte(tt.rootYAML), &root)
			assert.NoError(t, err)

			err = sanitizeHelmValues(root, tt.componentMaps)
			assert.NoError(t, err)

			assert.Equal(t, tt.expectRoot, root)
		})
	}
}
