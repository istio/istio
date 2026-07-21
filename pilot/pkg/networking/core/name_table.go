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
	"strings"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
	"istio.io/istio/pkg/util/sets"
)

var deltaAwareNdsConfigs = sets.New(
	kind.ServiceEntry,
	kind.DNSName,
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *dnsProto.NameTable {
	return dnsServer.BuildNameTable(dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	})
}

// BuildDeltaNameTable generates the per-host NDS resource deltas for a proxy.
// It handles the delta gate and falls back to a full build when needed.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	// this agent does not support or is requesting to not receive true Delta NDS.
	if !bool(proxy.Metadata.DeltaNDS) {
		nt := configgen.BuildNameTable(proxy, updates.Push)
		return []*discovery.Resource{{Resource: protoconv.MessageToAny(nt)}}, nil, model.DefaultXdsLogDetails, false
	}

	// at least one config kind has changed that doesn't support delta, we need to rebuild the whole table and send
	// each table entry
	if !shouldUseNdsDelta(updates) {
		nt := configgen.BuildNameTable(proxy, updates.Push)
		return toPerHostResources(nt.GetTable()), nil, model.DefaultXdsLogDetails, false
	}

	// True delta: build only the changed hostnames' records.
	changedHosts := ndsChangedHostnames(updates.ConfigsUpdated)
	newTable := dnsServer.BuildNameTable(dnsServer.Config{
		Node:                        proxy,
		Push:                        updates.Push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
		Hostnames:                   changedHosts,
	}).GetTable()

	res := toPerHostResources(newTable)
	var removed []string
	for name := range watched.ResourceNames {
		if _, stillPresent := newTable[name]; !stillPresent && ownedByAny(name, changedHosts) {
			removed = append(removed, name)
		}
	}
	return res, removed, model.XdsLogDetails{Incremental: true}, true
}

// shouldUseNdsDelta reports whether the push can be handled incrementally: not forced and
// containing only delta-aware, service-scoped kinds.
func shouldUseNdsDelta(updates *model.PushRequest) bool {
	if updates == nil || updates.Forced {
		return false
	}
	for k := range updates.ConfigsUpdated {
		if !deltaAwareNdsConfigs.Contains(k.Kind) {
			return false
		}
	}
	return true
}

func ndsChangedHostnames(cfgs sets.Set[model.ConfigKey]) sets.String {
	hosts := sets.NewWithLength[string](len(cfgs))
	for k := range cfgs {
		if deltaAwareNdsConfigs.Contains(k.Kind) {
			hosts.Insert(k.Name)
		}
	}
	return hosts
}

// ownedByAny reports whether name is owned by any of the given service hostnames: either name
// equals a hostname, or it is a single-label prefix of one (the per-pod headless records,
// e.g. mysql-0.mysql.default.svc.cluster.local owned by mysql.default.svc.cluster.local).
func ownedByAny(name string, hosts sets.String) bool {
	for h := range hosts {
		if isOwnedBy(name, h) {
			return true
		}
	}
	return false
}

// isOwnedBy reports whether name is owned by service hostname h.
func isOwnedBy(name, h string) bool {
	if name == h {
		return true
	}
	label, ok := strings.CutSuffix(name, "."+h)
	return ok && label != "" && !strings.Contains(label, ".")
}

// toPerHostResources decomposes a NameTable map into one discovery.Resource per hostname.
func toPerHostResources(table map[string]*dnsProto.NameTable_NameInfo) []*discovery.Resource {
	res := make([]*discovery.Resource, 0, len(table))
	for name, ni := range table {
		nt := &dnsProto.NameTable{Table: map[string]*dnsProto.NameTable_NameInfo{name: ni}}
		res = append(res, &discovery.Resource{
			Name:     name,
			Resource: protoconv.MessageToAny(nt),
		})
	}
	return res
}
