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

package validation

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/operator/pkg/apis/istio/v1alpha1"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		toValidate *v1alpha1.Values
		validated  bool
	}{
		{
			name:       "Empty struct",
			toValidate: &v1alpha1.Values{},
			validated:  true,
		},
		{
			name: "With CNI defined",
			toValidate: &v1alpha1.Values{
				Cni: &v1alpha1.CNIConfig{
					Enabled: &types.BoolValue{Value: true},
				},
			},
			validated: true,
		},
		{
			name: "With Slice",
			toValidate: &v1alpha1.Values{
				Gateways: &v1alpha1.GatewaysConfig{
					Enabled: &types.BoolValue{Value: true},
					IstioEgressgateway: &v1alpha1.EgressGatewayConfig{
						Ports: []*v1alpha1.PortsConfig{
							{
								Name: "port1",
							},
							{
								Name: "port2",
							},
						},
					},
				},
			},
			validated: true,
		},
	}

	for _, tt := range tests {
		err := validateSubTypes(reflect.ValueOf(tt.toValidate).Elem(), false, tt.toValidate, nil)
		if len(err) != 0 && tt.validated {
			t.Fatalf("Test %s failed with errors: %+v but supposed to succeed", tt.name, err)
		}
		if len(err) == 0 && !tt.validated {
			t.Fatalf("Test %s failed as it is supposed to fail but succeeded", tt.name)
		}
	}
}

func TestValidateFeatures(t *testing.T) {
	tests := []struct {
		name       string
		toValidate *v1alpha1.Values
		validated  bool
	}{
		{
			name: "automtls checks control plane security",
			toValidate: &v1alpha1.Values{
				Global: &v1alpha1.GlobalConfig{
					ControlPlaneSecurityEnabled: &types.BoolValue{Value: false},
					Mtls: &v1alpha1.MTLSConfig{
						Auto: &types.BoolValue{Value: true},
					},
				},
			},
			validated: false,
		},
	}
	for _, tt := range tests {
		err := validateFeatures(tt.toValidate, nil)
		if len(err) != 0 && tt.validated {
			t.Fatalf("Test %s failed with errors: %+v but supposed to succeed", tt.name, err)
		}
		if len(err) == 0 && !tt.validated {
			t.Fatalf("Test %s failed as it is supposed to fail but succeeded", tt.name)
		}
	}
}
