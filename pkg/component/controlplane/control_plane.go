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

package controlplane

import (
	"fmt"
	"strings"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/feature"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
)

// IstioControlPlane is an installation of an Istio control plane.
type IstioControlPlane struct {
	features []feature.IstioFeature
	started  bool
}

// NewIstioControlPlane creates a new IstioControlPlane and returns a pointer to it.
func NewIstioControlPlane(installSpec *v1alpha2.IstioControlPlaneSpec, translator *translate.Translator) *IstioControlPlane {
	opts := &feature.Options{
		InstallSpec: installSpec,
		Traslator:   translator,
	}
	return &IstioControlPlane{
		features: []feature.IstioFeature{
			feature.NewTrafficManagementFeature(opts),
			feature.NewSecurityFeature(opts),
			feature.NewPolicyFeature(opts),
			feature.NewTelemetryFeature(opts),
			feature.NewConfigManagementFeature(opts),
			feature.NewAutoInjectionFeature(opts),
		},
	}
}

// Run starts the Istio control plane.
func (i *IstioControlPlane) Run() error {
	for _, f := range i.features {
		if err := f.Run(); err != nil {
			return err
		}
	}
	i.started = true
	return nil
}

// RenderManifest returns a manifest rendered against
func (i *IstioControlPlane) RenderManifest() (manifest string, errsOut util.Errors) {
	if !i.started {
		return "", util.NewErrs(fmt.Errorf("istioControlPlane must be Run before calling RenderManifest"))
	}
	var sb strings.Builder
	for _, f := range i.features {
		s, errs := f.RenderManifest()
		errsOut = util.AppendErrs(errsOut, errs)
		_, err := sb.WriteString(s)
		errsOut = util.AppendErr(errsOut, err)
	}
	if len(errsOut) > 0 {
		return "", errsOut
	}
	return sb.String(), nil
}
