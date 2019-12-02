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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schemas"
)

func ProxyNeedsPush(proxy *model.Proxy, pushEv *XdsEvent) bool {
	if !features.ScopePushes.Get() {
		// If push scoping is not enabled, we push for all proxies
		return true
	}

	targetNamespaces := pushEv.namespacesUpdated
	configs := pushEv.configTypesUpdated

	// appliesToProxy starts as false, we will set it to true if we encounter any configs that require a push
	appliesToProxy := false
	// If no config specified, this request applies to all proxies
	if len(configs) == 0 {
		appliesToProxy = true
	}
Loop:
	for config := range configs {
		switch config {
		case schemas.Gateway.Type:
			if proxy.Type == model.Router {
				return true
			}
		case schemas.QuotaSpec.Type, schemas.QuotaSpecBinding.Type:
			if proxy.Type == model.SidecarProxy {
				return true
			}
		default:
			appliesToProxy = true
			break Loop
		}
	}

	if !appliesToProxy {
		return false
	}

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
	if !features.ScopePushes.Get() || len(pushEv.configTypesUpdated) == 0 {
		out[CDS] = true
		out[EDS] = true
		out[LDS] = true
		out[RDS] = true
		return out
	}

	// Note: CDS push must be followed by EDS, otherwise after Cluster is warmed, no ClusterLoadAssignment is retained.

	if proxy.Type == model.SidecarProxy {
		for config := range pushEv.configTypesUpdated {
			switch config {
			case schemas.VirtualService.Type:
				out[LDS] = true
				out[RDS] = true
			case schemas.Gateway.Type:
				// Do not push
			case schemas.ServiceEntry.Type, schemas.SyntheticServiceEntry.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case schemas.DestinationRule.Type:
				out[CDS] = true
				out[EDS] = true
			case schemas.EnvoyFilter.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case schemas.Sidecar.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case schemas.QuotaSpec.Type, schemas.QuotaSpecBinding.Type:
				// LDS must be pushed, otherwise RDS is not reloaded
				out[LDS] = true
				out[RDS] = true
			case schemas.AuthenticationPolicy.Type, schemas.AuthenticationMeshPolicy.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			case schemas.ServiceRole.Type, schemas.ServiceRoleBinding.Type, schemas.RbacConfig.Type,
				schemas.ClusterRbacConfig.Type, schemas.AuthorizationPolicy.Type:
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
		for config := range pushEv.configTypesUpdated {
			switch config {
			case schemas.VirtualService.Type:
				out[LDS] = true
				out[RDS] = true
			case schemas.Gateway.Type:
				out[LDS] = true
				out[RDS] = true
			case schemas.ServiceEntry.Type, schemas.SyntheticServiceEntry.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case schemas.DestinationRule.Type:
				out[CDS] = true
				out[EDS] = true
			case schemas.EnvoyFilter.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
				out[RDS] = true
			case schemas.Sidecar.Type, schemas.QuotaSpec.Type, schemas.QuotaSpecBinding.Type:
				// do not push for gateway
			case schemas.AuthenticationPolicy.Type, schemas.AuthenticationMeshPolicy.Type:
				out[CDS] = true
				out[EDS] = true
				out[LDS] = true
			case schemas.ServiceRole.Type, schemas.ServiceRoleBinding.Type, schemas.RbacConfig.Type,
				schemas.ClusterRbacConfig.Type, schemas.AuthorizationPolicy.Type:
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
