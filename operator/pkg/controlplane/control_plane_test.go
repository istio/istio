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
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
)

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
				components: []*component.IstioComponent{
					{
						Options:       coreComponentOptions,
						ComponentName: name.IstioBaseComponentName,
					},
					{
						Options:       coreComponentOptions,
						ResourceName:  "test-resource",
						ComponentName: name.PilotComponentName,
					},
					{
						ComponentName: name.CNIComponentName,
						Options:       coreComponentOptions,
					},
					{
						ComponentName: name.IstiodRemoteComponentName,
						Options:       coreComponentOptions,
					},
					{
						ComponentName: name.ZtunnelComponentName,
						Options:       coreComponentOptions,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotOperator, err := NewIstioControlPlane(tt.inInstallSpec, tt.inTranslator, nil, nil)
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
				components: []*component.IstioComponent{
					{
						Options:       coreComponentOptions,
						ComponentName: name.IstioBaseComponentName,
					},
					{
						Options:       coreComponentOptions,
						ResourceName:  "test-resource",
						ComponentName: name.PilotComponentName,
					},
					{
						ComponentName: name.CNIComponentName,
						Options:       coreComponentOptions,
					},
				},
				started: true,
			},
			wantManifests: map[name.ComponentName][]string{},
			wantErrs: util.Errors{
				fmt.Errorf("component Base not started in RenderManifest"),
				fmt.Errorf("component Pilot not started in RenderManifest"),
				fmt.Errorf("component Cni not started in RenderManifest"),
			},
		},
		{
			desc: "operator-not-started",
			testOperator: &IstioControlPlane{
				components: []*component.IstioComponent{
					{
						Options:       coreComponentOptions,
						ComponentName: name.IstioBaseComponentName,
					},
					{
						Options:       coreComponentOptions,
						ResourceName:  "test-resource",
						ComponentName: name.PilotComponentName,
					},
					{
						ComponentName: name.CNIComponentName,
						Options:       coreComponentOptions,
					},
				},
				started: false,
			},
			wantManifests: map[name.ComponentName][]string{},
			wantErrs: util.Errors{
				fmt.Errorf("istioControlPlane must be Run before calling RenderManifest"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotManifests, gotErrs := tt.testOperator.RenderManifest()
			if !reflect.DeepEqual(gotManifests, tt.wantManifests) || !util.EqualErrors(gotErrs, tt.wantErrs) {
				// reflect.DeepEqual returns false on size 0 maps
				if !(len(gotManifests) == 0) && (len(tt.wantManifests) == 0) {
					t.Errorf("%s: expected manifest map %+v errs %+v, got manifest map %+v errs %+v",
						tt.desc, tt.wantManifests, tt.wantErrs, gotManifests, gotErrs)
				}
			}
		})
	}
}
