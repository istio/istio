// Copyright Istio Authors.
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

package cmd

import (
	"sort"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/api/operator/v1alpha1"
)

func TestGetEnabledComponentsFromIOPSpec(t *testing.T) {
	enabledPbVal := &types.BoolValue{Value: true}
	disabledPbVal := &types.BoolValue{Value: false}

	for _, test := range []struct {
		name     string
		iops     *v1alpha1.IstioOperatorSpec
		expected []string
	}{
		{
			name:     "iop spec is nil",
			iops:     nil,
			expected: []string{},
		},
		{
			name: "all components enabled",
			iops: &v1alpha1.IstioOperatorSpec{
				Components: &v1alpha1.IstioComponentSetSpec{
					Base:  &v1alpha1.BaseComponentSpec{Enabled: enabledPbVal},
					Pilot: &v1alpha1.ComponentSpec{Enabled: enabledPbVal},
					Cni:   &v1alpha1.ComponentSpec{Enabled: enabledPbVal},
					IngressGateways: []*v1alpha1.GatewaySpec{
						{Name: "ingressgateway", Enabled: enabledPbVal},
						{Name: "eastwestgateway", Enabled: enabledPbVal},
					},
					EgressGateways: []*v1alpha1.GatewaySpec{
						{Name: "egressgateway", Enabled: enabledPbVal},
					},
				},
			},
			expected: []string{
				"base", "istiod", "cni",
				"ingress:ingressgateway", "ingress:eastwestgateway",
				"egress:egressgateway",
			},
		},
		{
			name: "cni and gateways are disabled",
			iops: &v1alpha1.IstioOperatorSpec{
				Components: &v1alpha1.IstioComponentSetSpec{
					Base:  &v1alpha1.BaseComponentSpec{Enabled: enabledPbVal},
					Pilot: &v1alpha1.ComponentSpec{Enabled: enabledPbVal},
					Cni:   &v1alpha1.ComponentSpec{Enabled: disabledPbVal},
					IngressGateways: []*v1alpha1.GatewaySpec{
						{Name: "ingressgateway", Enabled: disabledPbVal},
					},
					EgressGateways: []*v1alpha1.GatewaySpec{
						{Name: "egressgateway", Enabled: disabledPbVal},
					},
				},
			},
			expected: []string{"base", "istiod"},
		},
		{
			name: "all components are disabled",
			iops: &v1alpha1.IstioOperatorSpec{
				Components: &v1alpha1.IstioComponentSetSpec{
					Base:  &v1alpha1.BaseComponentSpec{Enabled: disabledPbVal},
					Pilot: &v1alpha1.ComponentSpec{Enabled: disabledPbVal},
					Cni:   &v1alpha1.ComponentSpec{Enabled: disabledPbVal},
					IngressGateways: []*v1alpha1.GatewaySpec{
						{Name: "ingressgateway", Enabled: disabledPbVal},
					},
					EgressGateways: []*v1alpha1.GatewaySpec{
						{Name: "egressgateway", Enabled: disabledPbVal},
					},
				},
			},
			expected: []string{},
		},
		{
			name: "component-spec has nil",
			iops: &v1alpha1.IstioOperatorSpec{
				Components: &v1alpha1.IstioComponentSetSpec{
					Base:  &v1alpha1.BaseComponentSpec{Enabled: enabledPbVal},
					Pilot: &v1alpha1.ComponentSpec{Enabled: enabledPbVal},
				},
			},
			expected: []string{"base", "istiod"},
		},
	} {
		t.Run(test.name, func(st *testing.T) {
			actual := getEnabledComponents(test.iops)
			sort.Strings(actual)
			sort.Strings(test.expected)
			if len(actual) != len(test.expected) {
				st.Fatalf("length of actual(%d) and expected(%d) don't match. "+
					"actual=%v, expected=%v", len(actual), len(test.expected), actual, test.expected)
			}
			for i := 0; i < len(actual); i++ {
				if actual[i] != test.expected[i] {
					st.Fatalf("actual %s does not match expected %s", actual[i], test.expected[i])
				}
			}
		})
	}
}
