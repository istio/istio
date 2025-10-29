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

// filterRelevantUpdates filters PushRequest.ConfigsUpdated so that only configs relevant to the proxy are included,
// returning the original PushRequest if no filtering was needed or a new PushRequest if some config filtering was performed.
// The returned PushRequest should not be modified since it might be the original global PushRequest.
func filterRelevantUpdates(proxy *model.Proxy, req *model.PushRequest) *model.PushRequest {
	if len(req.ConfigsUpdated) == 0 {
		return req
	}

	relevantUpdates := make(sets.Set[model.ConfigKey])
	changed := false
	for config := range req.ConfigsUpdated {
		if proxyDependentOnConfig(proxy, config, req.Push) {
			relevantUpdates.Insert(config)
		} else {
			// we have filtered out a config
			changed = true
		}
	}

	// If the proxy's service updated, need push for it.
	// Use GetServiceTargetsSnapshot to avoid race conditions with concurrent updates.
	serviceTargets := proxy.GetServiceTargetsSnapshot()
	if len(serviceTargets) > 0 && req.ConfigsUpdated != nil {
		for _, svc := range serviceTargets {
			key := model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(svc.Service.Hostname),
				Namespace: svc.Service.Attributes.Namespace,
			}
			if req.ConfigsUpdated.Contains(key) {
				relevantUpdates.Insert(key)
			}
		}
	}

	if changed {
		newPushRequest := *req
		newPushRequest.ConfigsUpdated = relevantUpdates
		return &newPushRequest
	}

	return req
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
			if features.FilterGatewayClusterConfig && !push.ServiceAttachedToGateway(config.Name, config.Namespace, proxy) {
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

// DefaultProxyNeedsPush check if a proxy needs push for this push event and returns a new PushRequest in the case
// it needs to filter relevant updates for this proxy.
func DefaultProxyNeedsPush(proxy *model.Proxy, req *model.PushRequest) (*model.PushRequest, bool) {
	if req.Forced {
		return req, true
	}

	if proxy.IsWaypointProxy() || proxy.IsZTunnel() {
		// Optimizations do not apply since scoping uses different mechanism
		// TODO: implement ambient aware scoping
		return req, true
	}

	req = filterRelevantUpdates(proxy, req)
	return req, len(req.ConfigsUpdated) > 0
}
