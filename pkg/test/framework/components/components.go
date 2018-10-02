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

package components

import (
	"istio.io/istio/pkg/test/framework/components/apiserver"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/citadel"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/components/policybackend"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/components/registry"
)

// Local components
var Local = registry.New()

// Kubernetes components
var Kubernetes = registry.New()

func init() {
	Local.Register(mixer.LocalComponent)
	Local.Register(pilot.LocalComponent)
	Local.Register(policybackend.LocalComponent)
	Local.Register(apps.LocalComponent)

	Kubernetes.Register(apiserver.KubeComponent)
	Kubernetes.Register(mixer.KubeComponent)
	Kubernetes.Register(pilot.KubeComponent)
	Kubernetes.Register(policybackend.KubeComponent)
	Kubernetes.Register(apps.KubeComponent)
	Kubernetes.Register(citadel.KubeComponent)
	Kubernetes.Register(bookinfo.KubeComponent)
	Kubernetes.Register(prometheus.KubeComponent)
	Kubernetes.Register(ingress.KubeComponent)
}
