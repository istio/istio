// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain ingressAdapter copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ingress

import (
	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngress(m *meshconfig.MeshConfig, r *resource.Entry) bool {
	class, exists := "", false
	if r.Metadata.Annotations != nil {
		class, exists = r.Metadata.Annotations[kube.IngressClassAnnotation]
	}

	switch m.IngressControllerMode {
	case meshconfig.MeshConfig_OFF:
		scope.Debugf("Skipping ingress due to Ingress Controller Mode OFF (%s)", r.Metadata.Name)
		return false
	case meshconfig.MeshConfig_STRICT:
		result := exists && class == m.IngressClass
		scope.Debugf("Checking ingress class w/ Strict (%s): %v", r.Metadata.Name, result)
		return result
	case meshconfig.MeshConfig_DEFAULT:
		result := !exists || class == m.IngressClass
		scope.Debugf("Checking ingress class w/ Default (%s): %v", r.Metadata.Name, result)
		return result
	default:
		scope.Warnf("invalid i synchronization mode: %v", m.IngressControllerMode)
		return false
	}
}
