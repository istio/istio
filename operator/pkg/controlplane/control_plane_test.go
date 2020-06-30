// Copyright 2020 Istio Authors
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

package controlplane

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
)

func TestOrderedKeys(t *testing.T) {
	tests := []struct {
		desc string
		in   map[string]*v1alpha1.ExternalComponentSpec
		want []string
	}{
		{
			desc: "not-ordered",
			in: map[string]*v1alpha1.ExternalComponentSpec{
				"graphql":   nil,
				"Abacus":    nil,
				"Astrology": nil,
				"gRPC":      nil,
				"blackjack": nil,
			},
			want: []string{
				"Abacus",
				"Astrology",
				"blackjack",
				"gRPC",
				"graphql",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := orderedKeys(tt.in); !(reflect.DeepEqual(got, tt.want)) {
				t.Errorf("%s: got %+v want %+v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestNewIstioOperator(t *testing.T) {
	coreComponentOptions := &component.Options{
		InstallSpec: &v1alpha1.IstioOperatorSpec{},
		Translator:  &translate.Translator{},
	}
	tests := []struct {
		desc              string
		inInstallSpec     *v1alpha1.IstioOperatorSpec
		inTranslator      *translate.Translator
		wantIstioOperator *IstioControlPlane
		wantErr           error
	}{
		{
			desc:          "core-components",
			inInstallSpec: &v1alpha1.IstioOperatorSpec{},
			inTranslator: &translate.Translator{
				ComponentMaps: map[name.ComponentName]*translate.ComponentMaps{
					"Pilot": {
						ResourceName: "test-resource",
					},
				},
			},
			wantErr: nil,
			wantIstioOperator: &IstioControlPlane{
				components: []component.IstioComponent{
					&component.BaseComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.IstioBaseComponentName,
						},
					},
					&component.PilotComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ResourceName:  "test-resource",
							ComponentName: name.PilotComponentName,
						},
					},
					&component.PolicyComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.PolicyComponentName,
						},
					},
					&component.TelemetryComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.TelemetryComponentName,
							Options:       coreComponentOptions,
						},
					},
					&component.CNIComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.CNIComponentName,
							Options:       coreComponentOptions,
						},
					},
					&component.IstiodRemoteComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.IstiodRemoteComponentName,
							Options:       coreComponentOptions,
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotOperator, err := NewIstioControlPlane(tt.inInstallSpec, tt.inTranslator)
			if ((err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil)) || !gotOperator.componentsEqual(tt.wantIstioOperator.components) {
				t.Errorf("%s: wanted components & err %+v %v, got components & err %+v %v",
					tt.desc, tt.wantIstioOperator.components, tt.wantErr, gotOperator.components, err)
			}
		})
	}
}

func TestIstioOperator_RenderManifest(t *testing.T) {
	coreComponentOptions := &component.Options{
		InstallSpec: &v1alpha1.IstioOperatorSpec{},
		Translator:  &translate.Translator{},
	}
	tests := []struct {
		desc          string
		testOperator  *IstioControlPlane
		wantManifests name.ManifestMap
		wantErrs      util.Errors
	}{
		{
			desc: "components-not-started-operator-started",
			testOperator: &IstioControlPlane{
				components: []component.IstioComponent{
					&component.BaseComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.IstioBaseComponentName,
						},
					},
					&component.PilotComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options: &component.Options{
								InstallSpec: &v1alpha1.IstioOperatorSpec{},
								Translator:  &translate.Translator{},
							},
							ResourceName:  "test-resource",
							ComponentName: name.PilotComponentName,
						},
					},
					&component.PolicyComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.PolicyComponentName,
						},
					},
					&component.TelemetryComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.TelemetryComponentName,
							Options:       coreComponentOptions,
						},
					},
					&component.CNIComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.CNIComponentName,
							Options:       coreComponentOptions,
						},
					},
				},
				started: true,
			},
			wantManifests: map[name.ComponentName][]string{},
			wantErrs: []error{
				fmt.Errorf("component Base not started in RenderManifest"),
				fmt.Errorf("component Pilot not started in RenderManifest"),
				fmt.Errorf("component Policy not started in RenderManifest"),
				fmt.Errorf("component Telemetry not started in RenderManifest"),
				fmt.Errorf("component Cni not started in RenderManifest"),
			},
		},
		{
			desc: "operator-not-started",
			testOperator: &IstioControlPlane{
				components: []component.IstioComponent{
					&component.BaseComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.IstioBaseComponentName,
						},
					},
					&component.PilotComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options: &component.Options{
								InstallSpec: &v1alpha1.IstioOperatorSpec{},
								Translator:  &translate.Translator{},
							},
							ResourceName:  "test-resource",
							ComponentName: name.PilotComponentName,
						},
					},
					&component.PolicyComponent{
						CommonComponentFields: &component.CommonComponentFields{
							Options:       coreComponentOptions,
							ComponentName: name.PolicyComponentName,
						},
					},
					&component.TelemetryComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.TelemetryComponentName,
							Options:       coreComponentOptions,
						},
					},
					&component.CNIComponent{
						CommonComponentFields: &component.CommonComponentFields{
							ComponentName: name.CNIComponentName,
							Options:       coreComponentOptions,
						},
					},
				},
				started: false,
			},
			wantManifests: map[name.ComponentName][]string{},
			wantErrs: []error{
				fmt.Errorf("istioControlPlane must be Run before calling RenderManifest"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotManifests, gotErrs := tt.testOperator.RenderManifest()
			if !reflect.DeepEqual(gotManifests, tt.wantManifests) || !reflect.DeepEqual(gotErrs, tt.wantErrs) {
				// reflect.DeepEqual returns false on size 0 maps
				if !(len(gotManifests) == 0) && (len(tt.wantManifests) == 0) {
					t.Errorf("%s: expected manifest map %+v errs %+v, got manifest map %+v errs %+v",
						tt.desc, tt.wantManifests, tt.wantErrs, gotManifests, gotErrs)
				}
			}
		})
	}
}
