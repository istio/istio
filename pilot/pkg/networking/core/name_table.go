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
	dnsutil "istio.io/istio/pkg/dns"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
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

// BuildDeltaNameTable returns a complete named snapshot or a collision-safe delta. Istiod
// materializes aliases so each resource can be updated independently by the agent.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	cfg := nameTableConfig(proxy, updates.Push)
	full := func() ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
		var previous *dnsServer.DeltaNameTable
		if watched != nil {
			previous, _ = watched.GeneratorState.(*dnsServer.DeltaNameTable)
		}
		state := dnsServer.NewDeltaNameTable(cfg)
		removed := state.Removed(previous)
		if watched != nil {
			// Delta NDS receives a private watch; the xDS server publishes this state after sending.
			watched.GeneratorState = state
		}
		resources := toPerNameResources(state.Table())
		// Delta xDS does not otherwise tell the agent that these resources replace its table.
		resources = append(resources, &discovery.Resource{
			Name:     dnsutil.FullSnapshotResourceName,
			Resource: protoconv.MessageToAny(&dnsProto.NameTable{}),
		})
		return resources, removed, model.DefaultXdsLogDetails, false
	}
	if !shouldUseNdsDelta(updates) || watched == nil || proxy.SidecarScope == nil {
		return full()
	}
	if legacyAutoAllocationRequiresFullRebuild(updates) {
		return full()
	}

	state, ok := watched.GeneratorState.(*dnsServer.DeltaNameTable)
	if !ok {
		return full()
	}
	updatedServices := sets.New[string]()
	reloadServices := sets.New[string]()
	// Endpoint-only updates reuse retained service groups; possible definition changes reload affected groups.
	for key := range updates.ConfigsUpdated {
		updatedServices.Insert(key.Name)
		if key.Kind == kind.ServiceEntry && !isHeadlessEndpointOnly(updates.Reason) {
			reloadServices.Insert(key.Name)
		}
	}
	if len(updatedServices) == 0 {
		return full()
	}

	changed, removed := state.Update(cfg, updatedServices, reloadServices)
	resources := toPerNameResources(changed)
	if len(resources) == 0 && len(removed) == 0 {
		return nil, nil, model.XdsLogDetails{Incremental: true}, true
	}
	return resources, removed, model.XdsLogDetails{Incremental: true}, true
}

// isHeadlessEndpointOnly reports whether a push contains only headless endpoint churn.
// EndpointUpdate may be coalesced with HeadlessEndpointUpdate; any other reason may indicate a service-definition change.
func isHeadlessEndpointOnly(reasons model.ReasonStats) bool {
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

func legacyAutoAllocationRequiresFullRebuild(updates *model.PushRequest) bool {
	// ServiceEntry definition changes can make the legacy allocator move unrelated addresses.
	return !features.EnableIPAutoallocate &&
		model.HasConfigsOfKind(updates.ConfigsUpdated, kind.ServiceEntry) &&
		!isHeadlessEndpointOnly(updates.Reason)
}

func nameTableConfig(node *model.Proxy, push *model.PushContext) dnsServer.Config {
	return dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	}
}

func shouldUseNdsDelta(updates *model.PushRequest) bool {
	if updates == nil || updates.Forced || len(updates.ConfigsUpdated) == 0 {
		return false
	}
	for config := range updates.ConfigsUpdated {
		if !deltaAwareNdsConfigs.Contains(config.Kind) {
			return false
		}
	}
	return true
}

func toPerNameResources(table map[string]*dnsProto.NameTable_NameInfo) []*discovery.Resource {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	slices.Sort(names)
	resources := make([]*discovery.Resource, 0, len(names))
	for _, name := range names {
		resources = append(resources, &discovery.Resource{
			Name:     name,
			Resource: protoconv.MessageToAny(&dnsProto.NameTable{NameInfo: table[name]}),
		})
	}
	return resources
}
