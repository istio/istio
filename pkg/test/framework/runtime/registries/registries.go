//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package registries

import (
	"fmt"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/apps"
	"istio.io/istio/pkg/test/framework/runtime/components/bookinfo"
	"istio.io/istio/pkg/test/framework/runtime/components/citadel"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native"
	"istio.io/istio/pkg/test/framework/runtime/components/ingress"
	"istio.io/istio/pkg/test/framework/runtime/components/mixer"
	"istio.io/istio/pkg/test/framework/runtime/components/pilot"
	"istio.io/istio/pkg/test/framework/runtime/components/policybackend"
	"istio.io/istio/pkg/test/framework/runtime/components/prometheus"
	"istio.io/istio/pkg/test/framework/runtime/registry"
)

var (
	// Native environment registry.
	Native = newEnvironment(descriptors.NativeEnvironment, native.NewEnvironment)

	// Kube environment registry.
	Kube = newEnvironment(descriptors.KubernetesEnvironment, kube.NewEnvironment)

	environmentMap = make(map[component.Variant]*registry.Instance)
)

func init() {
	// Register native components.
	Native.Register(descriptors.Apps, true, apps.NewNativeComponent)
	Native.Register(descriptors.Mixer, true, mixer.NewNativeComponent)
	Native.Register(descriptors.Pilot, true, pilot.NewNativeComponent)
	Native.Register(descriptors.PolicyBackend, true, policybackend.NewNativeComponent)

	// Register kubernetes components.
	Kube.Register(descriptors.Apps, true, apps.NewKubeComponent)
	Kube.Register(descriptors.BookInfo, true, bookinfo.NewKubeComponent)
	Kube.Register(descriptors.Citadel, true, citadel.NewKubeComponent)
	Kube.Register(descriptors.Ingress, true, ingress.NewKubeComponent)
	Kube.Register(descriptors.Mixer, true, mixer.NewKubeComponent)
	Kube.Register(descriptors.Pilot, true, pilot.NewKubeComponent)
	Kube.Register(descriptors.PolicyBackend, true, policybackend.NewKubeComponent)
	Kube.Register(descriptors.Prometheus, true, prometheus.NewKubeComponent)
}

// ForEnvironment returns the registry for the given environment
func ForEnvironment(e component.Variant) *registry.Instance {
	return environmentMap[e]
}

// GetSupportedEnvironments returns the list of registered environments.
func GetSupportedEnvironments() []component.Variant {
	variants := make([]component.Variant, 0, len(environmentMap))
	for v := range environmentMap {
		variants = append(variants, v)
	}
	return variants
}

func newEnvironment(desc component.Descriptor, factory api.ComponentFactory) *registry.Instance {
	r := registry.New()

	// Register the environment component in the registry.
	r.Register(desc, true, factory)

	if desc.Variant == "" {
		panic("failed registering environment without variant")
	}
	if _, ok := environmentMap[desc.Variant]; ok {
		panic(fmt.Sprintf("failed registering environment for duplicate variant: %s", desc.Variant))
	}
	environmentMap[desc.Variant] = r
	return r
}
