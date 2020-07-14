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

package validation

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	v1alpha12 "istio.io/api/operator/v1alpha1"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name     string
		value    *v1alpha12.IstioOperatorSpec
		warnings string
	}{
		{
			name: "addons",
			value: &v1alpha12.IstioOperatorSpec{
				AddonComponents: map[string]*v1alpha12.ExternalComponentSpec{
					"grafana": {
						Enabled: &v1alpha12.BoolValueForPB{BoolValue: types.BoolValue{Value: true}},
					},
				},
				Values: map[string]interface{}{
					"grafana": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			warnings: `! values.grafana.enabled is deprecated; use the samples/addons/ deployments instead
! addonComponents.grafana.enabled is deprecated; use the samples/addons/ deployments instead`,
		},
		{
			name: "global",
			value: &v1alpha12.IstioOperatorSpec{
				Values: map[string]interface{}{
					"global": map[string]interface{}{
						"localityLbSetting": map[string]interface{}{"foo": "bar"},
					},
				},
			},
			warnings: `! values.global.localityLbSetting is deprecated; use meshConfig.localityLbSetting instead`,
		},
		{
			name: "mixer",
			value: &v1alpha12.IstioOperatorSpec{
				Values: map[string]interface{}{
					"telemetry": map[string]interface{}{
						"v1": map[string]interface{}{"enabled": true},
					},
				},
				Components: &v1alpha12.IstioComponentSetSpec{
					Telemetry: &v1alpha12.ComponentSpec{
						Enabled: &v1alpha12.BoolValueForPB{BoolValue: types.BoolValue{Value: true}},
					},
				},
			},
			warnings: "! Values.telemetry.v1.enabled, Components.Telemetry.Enabled is deprecated." +
				" Mixer is deprecated and will be removed from Istio with the 1.8 release." +
				" Please consult our docs on the replacement.",
		},
		{
			name: "default_mixer_settings",
			value: &v1alpha12.IstioOperatorSpec{
				Values: map[string]interface{}{
					"telemetry": map[string]interface{}{
						"v1": map[string]interface{}{"enabled": false},
					},
				},
				Components: &v1alpha12.IstioComponentSetSpec{
					Telemetry: &v1alpha12.ComponentSpec{
						Enabled: &v1alpha12.BoolValueForPB{BoolValue: types.BoolValue{Value: false}},
					},
					Policy: &v1alpha12.ComponentSpec{
						Enabled: &v1alpha12.BoolValueForPB{BoolValue: types.BoolValue{Value: false}},
					},
				},
			},
			warnings: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, warnings := ValidateConfig(false, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if tt.warnings != warnings {
				t.Fatalf("expected warnings: %q got %q", tt.warnings, warnings)
			}
		})
	}
}

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
