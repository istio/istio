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

package helmreconciler

import (
	"fmt"

	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/validate"
)

// RenderCharts renders charts for h.
func (h *HelmReconciler) RenderCharts() (name.ManifestMap, error) {
	iopSpec := h.iop.Spec
	if err := validate.CheckIstioOperatorSpec(iopSpec, false); err != nil {
		if !h.opts.Force {
			return nil, err
		}
		h.opts.Log.PrintErr(fmt.Sprintf("spec invalid; continuing because of --force: %v\n", err))
	}

	t := translate.NewTranslator()

	cp, err := controlplane.NewIstioControlPlane(iopSpec, t)
	if err != nil {
		return nil, err
	}
	if err := cp.Run(); err != nil {
		return nil, fmt.Errorf("failed to create Istio control plane with spec: \n%v\nerror: %s", iopSpec, err)
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		err = errs.ToError()
	}

	h.manifests = manifests

	return manifests, err
}
