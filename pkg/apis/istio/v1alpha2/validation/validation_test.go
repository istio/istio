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

	"istio.io/operator/pkg/apis/istio/v1alpha2"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		toValidate *v1alpha2.Values
		validated  bool
	}{
		{
			name:       "Empty struct",
			toValidate: &v1alpha2.Values{},
			validated:  true,
		},
		{
			name: "With CNI defined",
			toValidate: &v1alpha2.Values{
				CNI: &v1alpha2.CNIConfig{
					Enabled: makeBoolPtr(true),
				},
			},
			validated: true,
		},
		{
			name: "With Slice",
			toValidate: &v1alpha2.Values{
				Gateways: &v1alpha2.GatewaysConfig{
					Enabled: makeBoolPtr(true),
					EgressGateway: &v1alpha2.EgressGatewayConfig{
						Ports: []*v1alpha2.PortsConfig{
							{
								Name: makeStringPtr("port1"),
							},
							{
								Name: makeStringPtr("port2"),
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

func makeBoolPtr(v bool) *bool {
	return &v
}
func makeStringPtr(v string) *string {
	return &v
}
