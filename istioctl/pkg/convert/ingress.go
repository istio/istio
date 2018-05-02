// Copyright 2018 Istio Authors.
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

package convert

import (
	"k8s.io/api/extensions/v1beta1"

	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
)

// IstioIngresses converts K8s extensions/v1beta1 Ingresses with Istio rules to v1alpha3 gateway and virtual service
func IstioIngresses(ingresses []*v1beta1.Ingress, domainSuffix string) ([]model.Config, error) {
	
	out := make([]model.Config, 0)

	for _, ingrezz := range ingresses {
		gateway, virtualService := ingress.ConvertIngressV1alpha3(*ingrezz, domainSuffix)
		out = append(out, gateway)
		out = append(out, virtualService)
	}

	return out, nil
}
