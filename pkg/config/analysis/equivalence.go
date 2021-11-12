/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package analysis

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var globals = map[collection.Schema]collection.Schema{
	collections.K8SExtensionsIstioIoV1Alpha1Wasmplugins:         collections.IstioExtensionsV1Alpha1Wasmplugins,
	collections.K8SNetworkingIstioIoV1Alpha3Destinationrules:    collections.IstioNetworkingV1Alpha3Destinationrules,
	collections.K8SNetworkingIstioIoV1Alpha3Envoyfilters:        collections.IstioNetworkingV1Alpha3Envoyfilters,
	collections.K8SNetworkingIstioIoV1Alpha3Gateways:            collections.IstioNetworkingV1Alpha3Gateways,
	collections.K8SNetworkingIstioIoV1Alpha3Serviceentries:      collections.IstioNetworkingV1Alpha3Serviceentries,
	collections.K8SNetworkingIstioIoV1Alpha3Sidecars:            collections.IstioNetworkingV1Alpha3Sidecars,
	collections.K8SNetworkingIstioIoV1Alpha3Virtualservices:     collections.IstioNetworkingV1Alpha3Virtualservices,
	collections.K8SNetworkingIstioIoV1Alpha3Workloadentries:     collections.IstioNetworkingV1Alpha3Workloadentries,
	collections.K8SNetworkingIstioIoV1Alpha3Workloadgroups:      collections.IstioNetworkingV1Alpha3Workloadgroups,
	collections.K8SNetworkingIstioIoV1Beta1Proxyconfigs:         collections.IstioNetworkingV1Beta1Proxyconfigs,
	collections.K8SSecurityIstioIoV1Beta1Authorizationpolicies:  collections.IstioSecurityV1Beta1Authorizationpolicies,
	collections.K8SSecurityIstioIoV1Beta1Peerauthentications:    collections.IstioSecurityV1Beta1Peerauthentications,
	collections.K8SSecurityIstioIoV1Beta1Requestauthentications: collections.IstioSecurityV1Beta1Requestauthentications,
	collections.K8STelemetryIstioIoV1Alpha1Telemetries:          collections.IstioTelemetryV1Alpha1Telemetries,
}

func AreEquivalent(one, two collection.Schema) bool {
	if eq, ok := globals[one]; ok {
		return two == eq
	}
	if eq, ok := globals[two]; ok {
		return one == eq
	}
	return one == two
}

func ContainmentMap(schemas collection.Schemas) map[collection.Name]struct{} {
	out := map[collection.Name]struct{}{}
	for schema := range ContainmentMapSchema(schemas) {
		out[schema.Name()] = struct{}{}
	}
	return out
}

func ContainmentMapSchema(schemas collection.Schemas) map[collection.Schema]struct{} {
	reverseMap := map[collection.Schema]collection.Schema{}
	for k, v := range globals {
		reverseMap[v] = k
	}
	out := map[collection.Schema]struct{}{}
	for _, schema := range schemas.All() {
		out[schema] = struct{}{}
		if val, ok := globals[schema]; ok {
			out[val] = struct{}{}
		} else if val, ok := reverseMap[schema]; ok {
			out[val] = struct{}{}
		}
	}
	return out
}
