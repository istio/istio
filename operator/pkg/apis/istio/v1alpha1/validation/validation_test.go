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
	v1alpha12 "istio.io/api/operator/v1alpha1"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
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
		name           string
		toValidateVals *v1alpha1.Values
		toValidateIOP  *v1alpha12.IstioOperatorSpec
		validated      bool
	}{
		{
			name: "automtls checks control plane security",
			toValidateVals: &v1alpha1.Values{
				Global: &v1alpha1.GlobalConfig{
					ControlPlaneSecurityEnabled: &types.BoolValue{Value: false},
				},
			},
			toValidateIOP: &v1alpha12.IstioOperatorSpec{
				MeshConfig: map[string]interface{}{
					"enableAutoMtls": true,
				},
			},
			validated: false,
		},
	}
	for _, tt := range tests {
		err := validateFeatures(tt.toValidateVals, tt.toValidateIOP)
		if len(err) != 0 && tt.validated {
			t.Fatalf("Test %s failed with errors: %+v but supposed to succeed", tt.name, err)
		}
		if len(err) == 0 && !tt.validated {
			t.Fatalf("Test %s failed as it is supposed to fail but succeeded", tt.name)
		}
	}
}
