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

// nolint: golint
package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	security_beta "istio.io/api/security/v1beta1"
	telemetry "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/kube"
)

func FuzzConfigValidation(data []byte) int {
	f := fuzz.NewConsumer(data)
	var iobj *config.Config
	configIndex, err := f.GetInt()
	if err != nil {
		return -1
	}

	r := collections.Pilot.All()[configIndex%len(collections.Pilot.All())]
	gvk := r.Resource().GroupVersionKind()
	kgvk := schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
	object, err := kube.IstioScheme.New(kgvk)
	if err != nil {
		return 0
	}
	_, err = apimeta.TypeAccessor(object)
	if err != nil {
		return 0
	}

	err = f.GenerateStruct(&object)
	if err != nil {
		return 0
	}

	iobj = crdclient.TranslateObject(object, gvk, "cluster.local")
	_, _ = r.Resource().ValidateConfig(*iobj)
	return 1
}

// FuzzConfigValidation2 implements a second fuzzer for config validation.
// The fuzzer targets the same API as FuzzConfigValidation above,
// but its approach to creating a fuzzed config is a bit different
// in that it utilizes Istio APIs to generate a Spec from json.
// We currently run both continuously to compare their performance.
func FuzzConfigValidation2(data []byte) int {
	f := fuzz.NewConsumer(data)
	configIndex, err := f.GetInt()
	if err != nil {
		return -1
	}
	r := collections.Pilot.All()[configIndex%len(collections.Pilot.All())]

	spec, err := r.Resource().NewInstance()
	if err != nil {
		return 0
	}
	jsonData, err := f.GetString()
	if err != nil {
		return 0
	}
	err = config.ApplyJSON(spec, jsonData)
	if err != nil {
		return 0
	}

	m := config.Meta{}
	err = f.GenerateStruct(&m)
	if err != nil {
		return 0
	}

	gvk := r.Resource().GroupVersionKind()
	m.GroupVersionKind = gvk

	_, _ = r.Resource().ValidateConfig(config.Config{
		Meta: m,
		Spec: spec,
	})
	return 1
}

func FuzzValidateGateWay(data []byte) int {
	in := &networking.Gateway{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateGateway(c)
	return 1
}

func FuzzValidateDestinationRule(data []byte) int {
	in := &networking.TrafficPolicy{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateDestinationRule(c)
	return 1
}

func FuzzValidateEnvoyFilter(data []byte) int {
	in := &networking.EnvoyFilter_EnvoyConfigObjectPatch{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateEnvoyFilter(c)
	return 1
}

func FuzzValidateSidecar(data []byte) int {
	in := &networking.Sidecar{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateSidecar(c)
	return 1
}

func FuzzValidateAuthorizationPolicy(data []byte) int {
	in := &security_beta.AuthorizationPolicy{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateAuthorizationPolicy(c)
	return 1
}

func FuzzValidateRequestAuthentication(data []byte) int {
	in := &security_beta.RequestAuthentication{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateRequestAuthentication(c)
	return 1
}

func FuzzValidatePeerAuthentication(data []byte) int {
	in := &security_beta.PeerAuthentication{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidatePeerAuthentication(c)
	return 1
}

func FuzzValidateVirtualService(data []byte) int {
	in := &networking.VirtualService{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateVirtualService(c)
	return 1
}

func FuzzValidateWorkloadEntry(data []byte) int {
	in := &networking.WorkloadEntry{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateWorkloadEntry(c)
	return 1
}

func FuzzValidateWorkloadGroup(data []byte) int {
	in := &networking.WorkloadGroup{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateWorkloadGroup(c)
	return 1
}

func FuzzValidateServiceEntry(data []byte) int {
	in := &networking.ServiceEntry{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateServiceEntry(c)
	return 1
}

func FuzzValidateProxyConfig(data []byte) int {
	in := &networkingv1beta1.ProxyConfig{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateProxyConfig(c)
	return 1
}

func FuzzValidateTelemetry(data []byte) int {
	in := &telemetry.Telemetry{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateTelemetry(c)
	return 1
}

func FuzzValidateWasmPlugin(data []byte) int {
	in := &extensions.WasmPlugin{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	c := config.Config{}
	err = f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	c.Spec = in
	_, _ = validation.ValidateWasmPlugin(c)
	return 1
}
