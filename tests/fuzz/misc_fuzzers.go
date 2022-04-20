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

// This file has a series of fuzzers that target different
// parts of Istio. They are placed here because it does not
// make sense to place them in different files yet.
// The fuzzers can be moved to other files without anything
// breaking on the OSS-fuzz side.

package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1/validation"
	"istio.io/istio/operator/pkg/controlplane"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/patch"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/validate"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
)

func FuzzCheckIstioOperatorSpec(data []byte) int {
	f := fuzz.NewConsumer(data)

	ispec := &v1alpha1.IstioOperatorSpec{}
	err := f.GenerateStruct(ispec)
	if err != nil {
		return 0
	}
	_ = validate.CheckIstioOperatorSpec(ispec, false)
	_ = validate.CheckIstioOperatorSpec(ispec, true)
	return 1
}

func FuzzV1Alpha1ValidateConfig(data []byte) int {
	f := fuzz.NewConsumer(data)

	iop := &v1alpha1.IstioOperatorSpec{}
	err := f.GenerateStruct(iop)
	if err != nil {
		return 0
	}
	_, _ = validation.ValidateConfig(false, iop)
	return 1
}

func FuzzGetEnabledComponents(data []byte) int {
	f := fuzz.NewConsumer(data)

	iopSpec := &v1alpha1.IstioOperatorSpec{}
	err := f.GenerateStruct(iopSpec)
	if err != nil {
		return 0
	}
	_, _ = translate.GetEnabledComponents(iopSpec)
	return 1
}

func FuzzUnmarshalAndValidateIOPS(data []byte) int {
	_, _ = istio.UnmarshalAndValidateIOPS(string(data))
	return 1
}

func FuzzRenderManifests(data []byte) int {
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()

	cp := &controlplane.IstioControlPlane{}
	err := f.GenerateStruct(cp)
	if err != nil {
		return 0
	}
	_, _ = cp.RenderManifest()
	return 1
}

func FuzzOverlayIOP(data []byte) int {
	f := fuzz.NewConsumer(data)

	base, err := f.GetString()
	if err != nil {
		return 0
	}
	overlay, err := f.GetString()
	if err != nil {
		return 0
	}
	_, _ = util.OverlayIOP(base, overlay)
	return 1
}

func FuzzNewControlplane(data []byte) int {
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()

	inInstallSpec := &v1alpha1.IstioOperatorSpec{}
	err := f.GenerateStruct(inInstallSpec)
	if err != nil {
		return 0
	}
	inTranslator := &translate.Translator{}
	err = f.GenerateStruct(inTranslator)
	if err != nil {
		return 0
	}
	if inTranslator.APIMapping == nil {
		return 0
	}
	if inTranslator.KubernetesMapping == nil {
		return 0
	}
	if inTranslator.GlobalNamespaces == nil {
		return 0
	}
	if inTranslator.ComponentMaps == nil {
		return 0
	}
	cm := &translate.ComponentMaps{}
	err = f.GenerateStruct(cm)
	if err != nil {
		return 0
	}
	inTranslator.ComponentMaps[name.PilotComponentName] = cm
	_, _ = controlplane.NewIstioControlPlane(inInstallSpec, inTranslator, nil)
	return 1
}

func FuzzResolveK8sConflict(data []byte) int {
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()

	ko1 := &object.K8sObject{}
	err := f.GenerateStruct(ko1)
	if err != nil {
		return 0
	}
	_ = ko1.ResolveK8sConflict()
	return 1
}

func FuzzYAMLManifestPatch(data []byte) int {
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()

	overlay := []*v1alpha1.K8SObjectOverlay{}
	number, err := f.GetInt()
	if err != nil {
		return 0
	}
	for i := 0; i < number%20; i++ {
		o := &v1alpha1.K8SObjectOverlay{}
		err := f.GenerateStruct(o)
		if err != nil {
			return 0
		}
		overlay = append(overlay, o)
	}
	baseYAML, err := f.GetString()
	if err != nil {
		return 0
	}
	defaultNamespace, err := f.GetString()
	if err != nil {
		return 0
	}

	_, _ = patch.YAMLManifestPatch(baseYAML, defaultNamespace, overlay)
	return 1
}

func FuzzGalleyDiag(data []byte) int {
	f := fuzz.NewConsumer(data)

	code, err := f.GetString()
	if err != nil {
		return 0
	}
	templ, err := f.GetString()
	if err != nil {
		return 0
	}
	mt := diag.NewMessageType(diag.Error, code, templ)
	resourceIsNil, err := f.GetBool()
	if err != nil {
		return 0
	}
	parameter, err := f.GetString()
	if err != nil {
		return 0
	}
	var ri *resource.Instance
	if resourceIsNil {
		ri = nil
	} else {
		err = f.GenerateStruct(ri)
		if err != nil {
			return 0
		}
	}
	m := diag.NewMessage(mt, ri, parameter)
	_ = m.Unstructured(true)
	_ = m.UnstructuredAnalysisMessageBase()
	_ = m.Origin()
	_, _ = m.MarshalJSON()
	_ = m.String()
	replStr, err := f.GetString()
	if err == nil {
		_ = m.ReplaceLine(replStr)
	}
	return 1
}
