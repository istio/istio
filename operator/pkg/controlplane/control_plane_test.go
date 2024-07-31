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

package controlplane

import (
	"reflect"
	"testing"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
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
	coreComponentOptions := &options{
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
				components: []*istioComponent{
					{
						options:       coreComponentOptions,
						ComponentName: name.IstioBaseComponentName,
					},
					{
						options:       coreComponentOptions,
						ResourceName:  "test-resource",
						ComponentName: name.PilotComponentName,
					},
					{
						ComponentName: name.CNIComponentName,
						options:       coreComponentOptions,
					},
					{
						ComponentName: name.IstiodRemoteComponentName,
						options:       coreComponentOptions,
					},
					{
						ComponentName: name.ZtunnelComponentName,
						options:       coreComponentOptions,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotOperator, err := NewIstioControlPlane(tt.inInstallSpec, tt.inTranslator, nil)
			if ((err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil)) || !gotOperator.componentsEqual(tt.wantIstioOperator.components) {
				t.Errorf("%s: wanted components & err %+v %v, got components & err %+v %v",
					tt.desc, tt.wantIstioOperator.components, tt.wantErr, gotOperator.components, err)
			}
		})
	}
}
