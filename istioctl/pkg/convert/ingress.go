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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
)

// IstioIngresses converts K8s extensions/v1beta1 Ingresses with Istio rules to v1alpha3 gateway and virtual service
func IstioIngresses(ingresses []*v1beta1.Ingress, domainSuffix string) ([]model.Config, error) {

	if len(ingresses) == 0 {
		return make([]model.Config, 0), nil
	}

	gateways := make([]model.Config, 0)
	virtualServices := make([]model.Config, 0)

	for _, ingrezz := range ingresses {
		gateway, virtualService := ingress.ConvertIngressV1alpha3(*ingrezz, domainSuffix)
		// Override the generated namespace; the supplied one is needed to resolve non-fully qualified hosts
		gateway.Namespace = ingrezz.Namespace
		virtualService.Namespace = ingrezz.Namespace
		gateways = append(gateways, gateway)
		virtualServices = append(virtualServices, virtualService)
	}

	merged := model.MergeGateways(gateways...)

	// Make a list of the servers.  Don't attempt any extra merging beyond MergeGateways() impl.
	allServers := make([]*networking.Server, 0)
	for _, servers := range merged.Servers {
		allServers = append(allServers, servers...)
	}

	// Convert the merged Gateway back into a model.Config
	mergedGateway := model.Config{
		ConfigMeta: gateways[0].ConfigMeta,
		Spec: &networking.Gateway{
			Servers:  allServers,
			Selector: map[string]string{"istio": "ingressgateway"},
		},
	}

	// Ensure the VirtualServices all point to mergedGateway
	for _, virtualService := range virtualServices {
		virtualService.Spec.(*networking.VirtualService).Gateways[0] = mergedGateway.ConfigMeta.Name
	}

	out := []model.Config{mergedGateway}
	out = append(out, virtualServices...)

	return out, nil
}
