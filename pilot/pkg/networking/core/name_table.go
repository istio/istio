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
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy. This is the legacy, single-resource format.
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *dnsProto.NameTable {
	return dnsServer.BuildNameTable(nameTableConfig(node, push))
}

// BuildNameTables returns one NDS resource per service hostname in the proxy's scope, named by that hostname.
func (configgen *ConfigGeneratorImpl) BuildNameTables(node *model.Proxy, push *model.PushContext) []*discovery.Resource {
	return toPerHostResources(dnsServer.BuildNameTablesByHostname(nameTableConfig(node, push), nil))
}

// BuildDeltaNameTable returns the NDS resources of the hostnames updated by a partial push, and the previously sent
// hostnames that no longer produce any name.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string) {
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
	if len(tables) == 0 && len(removed) == 0 {
		return nil, nil
	}
	return toPerHostResources(tables), slices.Sort(removed)
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
	for _, hostname := range slices.Sort(maps.Keys(tables)) {
		resources = append(resources, &discovery.Resource{
			Name:     hostname,
			Resource: protoconv.MessageToAny(tables[hostname]),
		})
	}
	return resources
}
