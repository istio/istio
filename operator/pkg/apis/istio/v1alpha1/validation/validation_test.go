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

package validation_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	v1alpha12 "istio.io/api/operator/v1alpha1"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1/validation"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/pkg/test/env"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, warnings := validation.ValidateConfig(false, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if tt.warnings != warnings {
				t.Fatalf("expected warnings: %q got %q", tt.warnings, warnings)
			}
		})
	}
}

func TestValidateProfiles(t *testing.T) {
	manifests := filepath.Join(env.IstioSrc, helm.OperatorSubdirFilePath)
	profiles, err := helm.ListProfiles(manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) < 2 {
		// Just ensure we find some profiles, in case this code breaks
		t.Fatalf("Maybe have failed getting profiles, got %v", profiles)
	}
	for _, tt := range profiles {
		t.Run(tt, func(t *testing.T) {
			_, s, err := manifest.GenIOPSFromProfile(tt, "", []string{"installPackagePath=" + manifests}, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			verr, warnings := validation.ValidateConfig(false, s)
			if verr != nil {
				t.Fatalf("got error validating: %v", verr)
			}
			if warnings != "" {
				t.Fatalf("got warning validating: %v", warnings)
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
		err := validation.ValidateSubTypes(reflect.ValueOf(tt.toValidate).Elem(), false, tt.toValidate, nil)
		if len(err) != 0 && tt.validated {
			t.Fatalf("Test %s failed with errors: %+v but supposed to succeed", tt.name, err)
		}
		if len(err) == 0 && !tt.validated {
			t.Fatalf("Test %s failed as it is supposed to fail but succeeded", tt.name)
		}
	}
}
