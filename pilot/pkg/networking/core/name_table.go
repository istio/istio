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

package core

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var deltaAwareNdsConfigs = sets.New(
	kind.ServiceEntry,
	kind.DNSName,
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy. This is the legacy, single-resource format.
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *dnsProto.NameTable {
	return dnsServer.BuildNameTable(nameTableConfig(node, push))
}

// BuildDeltaNameTable returns one NDS resource per service hostname, named by that hostname, and the removed
// hostnames. Full pushes leave removals to the xDS server, which diffs against the resources previously sent.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	cfg := nameTableConfig(proxy, updates.Push)
	if requiresFullPush(proxy, updates, watched) {
		return toPerHostResources(dnsServer.BuildNameTablesByHostname(cfg, nil)), nil, model.DefaultXdsLogDetails, false
	}
	hostnames := sets.NewWithLength[string](len(updates.ConfigsUpdated))
	for key := range updates.ConfigsUpdated {
		hostnames.Insert(key.Name)
	}
	tables := dnsServer.BuildNameTablesByHostname(cfg, hostnames)
	var removed []string
	for hostname := range hostnames {
		if _, found := tables[hostname]; !found && watched.ResourceNames.Contains(hostname) {
			removed = append(removed, hostname)
		}
	}
	if len(tables) == 0 && len(removed) == 0 {
		return nil, nil, model.XdsLogDetails{Incremental: true}, true
	}
	slices.Sort(removed)
	return toPerHostResources(tables), removed, model.XdsLogDetails{Incremental: true}, true
}

// IsHeadlessEndpointOnly reports whether a push contains only headless endpoint churn.
// EndpointUpdate may be coalesced with HeadlessEndpointUpdate; any other reason may indicate a service-definition change.
func IsHeadlessEndpointOnly(reasons model.ReasonStats) bool {
	if !reasons.Has(model.HeadlessEndpointUpdate) {
		return false
	}
	for reason := range reasons {
		if reason != model.HeadlessEndpointUpdate && reason != model.EndpointUpdate {
			return false
		}
	}
	return true
}

// legacyAutoAllocationRequiresFullRebuild reports whether a push can reassign addresses outside its changed services.
func legacyAutoAllocationRequiresFullRebuild(updates *model.PushRequest) bool {
	// ServiceEntry definition changes can make the legacy allocator move unrelated addresses.
	return !features.EnableIPAutoallocate &&
		model.HasConfigsOfKind(updates.ConfigsUpdated, kind.ServiceEntry) &&
		!IsHeadlessEndpointOnly(updates.Reason)
}

func nameTableConfig(node *model.Proxy, push *model.PushContext) dnsServer.Config {
	return dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	}
}

// requiresFullPush reports whether Delta NDS must send all resources instead of an incremental update.
func requiresFullPush(proxy *model.Proxy, updates *model.PushRequest, watched *model.WatchedResource) bool {
	if updates == nil || updates.Forced || len(updates.ConfigsUpdated) == 0 || watched == nil || proxy.SidecarScope == nil {
		return true
	}
	if legacyAutoAllocationRequiresFullRebuild(updates) {
		return true
	}
	for config := range updates.ConfigsUpdated {
		if !deltaAwareNdsConfigs.Contains(config.Kind) {
			return true
		}
	}
	return false
}

func toPerHostResources(tables map[string]*dnsProto.NameTable) []*discovery.Resource {
	resources := make([]*discovery.Resource, 0, len(tables))
	for _, hostname := range slices.Sort(maps.Keys(tables)) {
		resources = append(resources, &discovery.Resource{
			Name:     hostname,
			Resource: protoconv.MessageToAny(tables[hostname]),
		})
	}
	return resources
}
