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
	"istio.io/istio/pkg/util/sets"
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy. This is the legacy, single-resource format.
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *dnsProto.NameTable {
	return dnsServer.BuildNameTable(nameTableConfig(node, push))
}

// Configs whose key name is the hostname of the name table they affect.
var deltaAwareNdsConfigs = sets.New(kind.ServiceEntry, kind.DNSName)

// BuildDeltaNameTable returns one NDS resource per hostname affected by the push, named by that hostname, and the
// previously sent hostnames that no longer produce any name. If the push can't be limited to the updated hostnames,
// it returns every hostname in the proxy's scope.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	if watched == nil || !canSendPartialNdsPush(updates) {
		// The xDS server removes the previously sent resources that are not part of a full push.
		tables := dnsServer.BuildNameTablesByHostname(nameTableConfig(proxy, updates.Push), nil)
		return toPerHostResources(tables), nil, model.DefaultXdsLogDetails, false
	}
	hostnames := sets.NewWithLength[string](len(updates.ConfigsUpdated))
	for key := range updates.ConfigsUpdated {
		hostnames.Insert(key.Name)
	}
	tables := dnsServer.BuildNameTablesByHostname(nameTableConfig(proxy, updates.Push), hostnames)
	var removed []string
	for hostname := range hostnames {
		if _, found := tables[hostname]; !found && watched.ResourceNames.Contains(hostname) {
			removed = append(removed, hostname)
		}
	}
	// Nil resources and removals skip the response.
	var resources []*discovery.Resource
	if len(tables) > 0 {
		resources = toPerHostResources(tables)
	}
	return resources, removed, model.XdsLogDetails{Incremental: true}, true
}

// canSendPartialNdsPush reports whether a push only needs the name tables of the updated hostnames.
func canSendPartialNdsPush(req *model.PushRequest) bool {
	if req.Forced || len(req.ConfigsUpdated) == 0 {
		return false
	}
	for cfg := range req.ConfigsUpdated {
		if !deltaAwareNdsConfigs.Contains(cfg.Kind) {
			return false
		}
	}
	return true
}

func nameTableConfig(node *model.Proxy, push *model.PushContext) dnsServer.Config {
	return dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	}
}

func toPerHostResources(tables map[string]*dnsProto.NameTable) []*discovery.Resource {
	resources := make([]*discovery.Resource, 0, len(tables))
	for hostname, table := range tables {
		resources = append(resources, &discovery.Resource{
			Name:     hostname,
			Resource: protoconv.MessageToAny(table),
		})
	}
	return resources
}
