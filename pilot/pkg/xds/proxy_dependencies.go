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

package xds

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// UnAffectedConfigKinds contains config types which does not affect certain proxy types.
var UnAffectedConfigKinds = map[model.NodeType]sets.Set[kind.Kind]{
	// For Gateways, we do not care about the following configs for example Sidecar.
	model.Router: sets.New(kind.Sidecar),
	// For Sidecar, we do not care about the following configs for example Gateway.
	model.SidecarProxy: sets.New(kind.Gateway),
}

// ConfigAffectsProxy checks if a pushEv will affect a specified proxy. That means whether the push will be performed
// towards the proxy.
func ConfigAffectsProxy(req *model.PushRequest, proxy *model.Proxy) bool {
	// Empty changes means "all" to get a backward compatibility.
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	if proxy.IsWaypointProxy() || proxy.IsZTunnel() {
		// Optimizations do not apply since scoping uses different mechanism
		// TODO: implement ambient aware scoping
		return true
	}

	for config := range req.ConfigsUpdated {
		if proxyDependentOnConfig(proxy, config, req.Push) {
			return true
		}
	}

	return false
}

func proxyDependentOnConfig(proxy *model.Proxy, config model.ConfigKey, push *model.PushContext) bool {
	// Skip config dependency check based on proxy type for certain configs.
	if UnAffectedConfigKinds[proxy.Type].Contains(config.Kind) {
		return false
	}
	// Detailed config dependencies check.
	switch proxy.Type {
	case model.SidecarProxy:
		if proxy.SidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		} else if proxy.PrevSidecarScope != nil && proxy.PrevSidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		}
	case model.Router:
		if config.Kind == kind.ServiceEntry {
			// If config is ServiceEntry, name of the config is service's FQDN
			if features.FilterGatewayClusterConfig && !push.ServiceAttachedToGateway(config.Name, proxy) {
				return false
			}

			hostname := host.Name(config.Name)
			// gateways have default sidecar scopes
			if proxy.SidecarScope.GetService(hostname) == nil &&
				proxy.PrevSidecarScope.GetService(hostname) == nil {
				// skip the push when the service is not visible to the gateway,
				// and the old service is not visible/existent
				return false
			}
		}
		return true
	default:
		// TODO We'll add the check for other proxy types later.
		return true
	}
	return false
}

// DefaultProxyNeedsPush check if a proxy needs push for this push event.
func DefaultProxyNeedsPush(proxy *model.Proxy, req *model.PushRequest) bool {
	if ConfigAffectsProxy(req, proxy) {
		return true
	}

	// If the proxy's service updated, need push for it.
	if len(proxy.ServiceTargets) > 0 && req.ConfigsUpdated != nil {
		for _, svc := range proxy.ServiceTargets {
			if _, ok := req.ConfigsUpdated[model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(svc.Service.Hostname),
				Namespace: svc.Service.Attributes.Namespace,
			}]; ok {
				return true
			}
		}
	}

	return false
}
