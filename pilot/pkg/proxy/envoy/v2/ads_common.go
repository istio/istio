// Copyright 2019 Istio Authors
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

package v2

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

var configKindAffectedProxyTypes = map[resource.GroupVersionKind][]model.NodeType{
	collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():           {model.Router},
	collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():        {model.SidecarProxy},
	collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind(): {model.SidecarProxy},
}

// ConfigAffectsProxy checks if a pushEv will affect a specified proxy. That means whether the push will be performed
// towards the proxy.
func ConfigAffectsProxy(pushEv *XdsEvent, proxy *model.Proxy) bool {
	// Empty changes means "all" to get a backward compatibility.
	if len(pushEv.configsUpdated) == 0 {
		return true
	}

	for config := range pushEv.configsUpdated {
		// If we've already know a specific configKind will affect some proxy types, check for that.
		if kindAffectedTypes, f := configKindAffectedProxyTypes[config.Kind]; f {
			for _, t := range kindAffectedTypes {
				if t == proxy.Type {
					return true
				}
			}
			continue
		}

		// Detailed config dependencies check.
		switch proxy.Type {
		case model.SidecarProxy:
			// Not scoping of all config types are known to SidecarScope. We consider the unknown cases as "affected".
			ok, scoped := proxy.SidecarScope.DependsOnConfig(config)
			if !scoped || ok {
				return true
			}
		// TODO We'll add the check for other proxy types later.
		default:
			return true
		}
	}

	return false
}

// ProxyNeedsPush check if a proxy needs push for this push event.
func ProxyNeedsPush(proxy *model.Proxy, pushEv *XdsEvent) bool {
	if !ConfigAffectsProxy(pushEv, proxy) {
		return false
	}

	targetNamespaces := pushEv.namespacesUpdated
	// If no only namespaces specified, this request applies to all proxies
	if len(targetNamespaces) == 0 {
		return true
	}

	// If the proxy's service updated, need push for it.
	if len(proxy.ServiceInstances) > 0 {
		ns := proxy.ServiceInstances[0].Service.Attributes.Namespace
		if _, ok := targetNamespaces[ns]; ok {
			return true
		}
	}

	// Otherwise, only apply if the egress listener will import the config present in the update
	for ns := range targetNamespaces {
		if proxy.SidecarScope.DependsOnNamespace(ns) {
			return true
		}
	}
	return false
}

type XdsType int

const (
	CDS XdsType = iota
	EDS
	LDS
	RDS
)

// TODO: merge with ProxyNeedsPush
func PushTypeFor(proxy *model.Proxy, pushEv *XdsEvent) map[XdsType]bool {
	out := map[XdsType]bool{}

	// In case configTypes is not set, for example mesh configuration updated.
	// If push scoping is not enabled, we push all xds
	if len(pushEv.configsUpdated) == 0 {
		out[CDS] = true
		out[EDS] = true
		out[LDS] = true
		out[RDS] = true
		return out
	}

	// Note: CDS push must be followed by EDS, otherwise after Cluster is warmed, no ClusterLoadAssignment is retained.

	if proxy.Type == model.SidecarProxy {
		for config := range pushEv.configsUpdated {
			switch config.Kind {
			case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
				// Do not push
			case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
			case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind(),
				collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
				// LDS must be pushed, otherwise RDS is not reloaded
				out[LDS] = true
				out[RDS] = true
			case collections.IstioAuthenticationV1Alpha1Policies.Resource().GroupVersionKind(),
				collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			case collections.IstioRbacV1Alpha1Serviceroles.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Servicerolebindings.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Rbacconfigs.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().GroupVersionKind(),
				collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
				collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
				out[LDS] = true
			case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			default:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			}
			// To return asap
			if len(out) == 4 {
				return out
			}
		}
	} else {
		for config := range pushEv.configsUpdated {
			switch config.Kind {
			case collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind():
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
			case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind(),
				collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind(),
				collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind():
				// do not push for gateway
			case collections.IstioAuthenticationV1Alpha1Policies.Resource().GroupVersionKind(),
				collections.IstioAuthenticationV1Alpha1Meshpolicies.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			case collections.IstioRbacV1Alpha1Serviceroles.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Servicerolebindings.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Rbacconfigs.Resource().GroupVersionKind(),
				collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().GroupVersionKind(),
				collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
				collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
				out[LDS] = true
			case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			default:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			}
			// To return asap
			if len(out) == 4 {
				return out
			}
		}
	}
	return out
}
