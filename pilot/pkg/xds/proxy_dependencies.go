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
	v3 "istio.io/istio/pilot/pkg/xds/v3"
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
			continue
		}
		// if proxy enables self-discovery, we need to always include LocalService related configs.
		// we can avoid this elsewhere to avoid unnecessary proxy pushes.
		if proxy.Metadata.EnableSelfDiscovery && (config.Kind == kind.ServiceEntry || config.Kind == kind.Endpoints) {
			if (config.Name == proxy.PrevLocalService.Name && config.Namespace == proxy.PrevLocalService.Namespace) ||
				(config.Name == proxy.LocalService.Name && config.Namespace == proxy.LocalService.Namespace) {
				relevantUpdates.Insert(config)
				continue
			}
		}
		// A filtered key can still be needed to fix a stale Delta NDS entry of the proxy. It is requeued as DNSName,
		// which only NDS pushes. For example:
		//   - Renaming a ServiceEntry host from old.com to new.com publishes new.com, then old.com. If they land in
		//     separate pushes, the first sends only new.com and by the second neither scope has old.com. old.com is
		//     requeued and removed.
		//   - On a gateway with PILOT_FILTER_GATEWAY_CLUSTER_CONFIG, a stale hostname that a VirtualService of the
		//     gateway routes to is requeued and removed. Other stale hostnames are left to the next full push.
		//   - Renaming h.com in namespace b, when a also has it, leaves b's address in h.com. h.com is requeued and
		//     rebuilt with only a's address.
		if key, ok := staleNameTableKey(proxy, config, req.Push); ok {
			relevantUpdates.Insert(key)
			changed = true
			continue
		}

		// we have filtered out a config
		changed = true
	}

	// If the proxy's service updated, need push for it.
	if len(proxy.ServiceTargets) > 0 && req.ConfigsUpdated != nil {
		for _, svc := range proxy.ServiceTargets {
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
	// Keep Address config keys only for AddressType subscribers. WorkloadType subscribers
	// are selected in DefaultProxyNeedsPush via AddressesUpdated without retaining config keys.
	// Other sidecar and gateway dependencies are surfaced as ServiceEntry or Endpoints updates.
	if features.ScopedAddressPushes && config.Kind == kind.Address {
		return proxy.GetWatchedResource(v3.AddressType) != nil
	}
	// Only NDS uses DNSName.
	if config.Kind == kind.DNSName && proxy.GetWatchedResource(v3.NameTableType) == nil {
		return false
	}
	// Detailed config dependencies check.
	switch proxy.Type {
	case model.Router:
		if filteredForGateway(proxy, config, push) {
			return false
		}
		// fallthrough to the standard sidecar proxy dependency check.
		fallthrough
	case model.SidecarProxy:
		if proxy.SidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		} else if proxy.PrevSidecarScope != nil && proxy.PrevSidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		}
	default:
		// TODO We'll add the check for other proxy types later.
		return true
	}
	return false
}

// filteredForGateway reports whether a gateway with filtered gateway clusters skips the config. Only services
// referenced by the proxy's gateways are pushed, whatever their visibility. The ServiceEntry key names the FQDN.
func filteredForGateway(proxy *model.Proxy, config model.ConfigKey, push *model.PushContext) bool {
	return proxy.Type == model.Router && config.Kind == kind.ServiceEntry && features.FilterGatewayClusterConfig &&
		!push.ServiceAttachedToGateway(config.Name, config.Namespace, proxy)
}

// staleNameTableKey returns the DNSName key for a ServiceEntry or DNSName key of a hostname the proxy has a Delta NDS
// resource for.
func staleNameTableKey(proxy *model.Proxy, config model.ConfigKey, push *model.PushContext) (model.ConfigKey, bool) {
	if (config.Kind != kind.ServiceEntry && config.Kind != kind.DNSName) ||
		!proxyNameTableHasHostname(proxy, config.Name) || filteredForGateway(proxy, config, push) {
		return model.ConfigKey{}, false
	}
	return model.ConfigKey{Kind: kind.DNSName, Name: config.Name, Namespace: config.Namespace}, true
}

// proxyNameTableHasHostname reports whether the proxy has a Delta NDS resource for the hostname.
func proxyNameTableHasHostname(proxy *model.Proxy, hostname string) bool {
	proxy.RLock()
	defer proxy.RUnlock()
	wr := proxy.WatchedResources[v3.NameTableType]
	return wr != nil && wr.ResourceNames.Contains(hostname)
}

// DefaultProxyNeedsPush check if a proxy needs push for this push event and returns a new PushRequest in the case
// it needs to filter relevant updates for this proxy.
func DefaultProxyNeedsPush(proxy *model.Proxy, req *model.PushRequest) (*model.PushRequest, bool) {
	if req.Forced {
		return req, true
	}

	if proxy.IsWaypointProxy() || proxy.IsZTunnel() || proxy.IsAgentgateway() {
		// Optimizations do not apply since scoping uses different mechanism
		// TODO: implement ambient aware scoping
		return req, true
	}

	req = filterRelevantUpdates(proxy, req)
	workloadsUpdated := len(req.AddressesUpdated) > 0 && proxy.GetWatchedResource(v3.WorkloadType) != nil
	return req, workloadsUpdated || len(req.ConfigsUpdated) > 0
}
