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

	iobj := crdclient.TranslateObject(object, gvk, "cluster.local")
	_, _ = r.Resource().ValidateConfig(iobj)
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

func FuzzConfigValidation3(data []byte) int {
	if len(data) < 10 {
		return 0
	}
	f := fuzz.NewConsumer(data)
	c := config.Config{}
	err := f.GenerateStruct(&c)
	if err != nil {
		return 0
	}
	targetNumber, err := f.GetInt()
	if err != nil {
		return 0
	}
	numberOfTargets := targetNumber % 13
	switch numberOfTargets {
	case 0:
		in := &networking.Gateway{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateGateway(c)
	case 1:
		in := &networking.TrafficPolicy{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateDestinationRule(c)
	case 2:
		in := &networking.EnvoyFilter_EnvoyConfigObjectPatch{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateEnvoyFilter(c)
	case 3:
		in := &networking.Sidecar{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateSidecar(c)
	case 4:
		in := &security_beta.AuthorizationPolicy{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateAuthorizationPolicy(c)
	case 5:
		in := &security_beta.RequestAuthentication{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateRequestAuthentication(c)
	case 6:
		in := &security_beta.PeerAuthentication{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidatePeerAuthentication(c)
	case 7:
		in := &networking.VirtualService{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateVirtualService(c)
	case 8:
		in := &networking.WorkloadEntry{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateWorkloadEntry(c)
	case 9:
		in := &networking.ServiceEntry{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateServiceEntry(c)
	case 10:
		in := &networkingv1beta1.ProxyConfig{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateProxyConfig(c)
	case 11:
		in := &telemetry.Telemetry{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateTelemetry(c)
	case 12:
		in := &extensions.WasmPlugin{}
		err = f.GenerateStruct(in)
		if err != nil {
			return 0
		}
		c.Spec = in
		_, _ = validation.ValidateWasmPlugin(c)
	}
	return 1
}
