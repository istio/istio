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
	"google.golang.org/protobuf/types/known/anypb"

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
		resources := toPerNameResources(state.Table(), 1)
		// Delta xDS does not otherwise tell the agent that these resources replace its table.
		resources = append(resources, &discovery.Resource{
			Name:     dnsutil.FullSnapshotResourceName,
			Resource: protoconv.MessageToAny(&dnsProto.NameTable{}),
		})
		return resources, removed, model.DefaultXdsLogDetails, false
	}
	if requiresFullPush(proxy, updates, watched) {
		return full()
	}
	state := watched.GeneratorState.(*dnsServer.DeltaNameTable)
	updatedServices := sets.New[string]()
	reloadServices := sets.New[string]()
	headlessEndpointOnly := IsHeadlessEndpointOnly(updates.Reason)
	// Endpoint-only updates reuse retained service groups; possible definition changes reload affected groups.
	for key := range updates.ConfigsUpdated {
		updatedServices.Insert(key.Name)
		if key.Kind == kind.ServiceEntry && !headlessEndpointOnly {
			reloadServices.Insert(key.Name)
		}
	}

	changed, removed := state.Update(cfg, updatedServices, reloadServices)
	resources := toPerNameResources(changed, 0)
	if len(resources) == 0 && len(removed) == 0 {
		return nil, nil, model.XdsLogDetails{Incremental: true}, true
	}
	return resources, removed, model.XdsLogDetails{Incremental: true}, true
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

// requiresFullPush reports whether Delta NDS must send an authoritative snapshot instead of an incremental update.
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
	state, ok := watched.GeneratorState.(*dnsServer.DeltaNameTable)
	return !ok || state == nil
}

func toPerNameResources(table map[string]*dnsProto.NameTable_NameInfo, extraCapacity int) []*discovery.Resource {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	slices.Sort(names)
	resources := make([]*discovery.Resource, 0, len(names)+extraCapacity)
	payloads := make(map[*dnsProto.NameTable_NameInfo]*anypb.Any, len(table))
	for _, name := range names {
		info := table[name]
		payload := payloads[info]
		if payload == nil {
			payload = protoconv.MessageToAny(&dnsProto.NameTable{NameInfo: info})
			payloads[info] = payload
		}
		resources = append(resources, &discovery.Resource{
			Name:     name,
			Resource: payload,
		})
	}
	return resources
}
