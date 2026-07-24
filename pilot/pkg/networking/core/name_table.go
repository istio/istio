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
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/host"
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
// will only be effective if DNS capture is enabled in the proxy. This is the NDS SotW output:
// the whole table wrapped in a single xDS resource.
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) ([]*discovery.Resource, model.XdsLogDetails) {
	nt := dnsServer.BuildNameTable(dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	})
	if nt == nil {
		return nil, model.DefaultXdsLogDetails
	}
	return []*discovery.Resource{{Resource: protoconv.MessageToAny(nt)}}, model.DefaultXdsLogDetails
}

// BuildDeltaNameTable generates the per-host NDS resource deltas for a proxy.
// It handles the delta gate and falls back to a full build when needed.
func (configgen *ConfigGeneratorImpl) BuildDeltaNameTable(proxy *model.Proxy, updates *model.PushRequest,
	watched *model.WatchedResource,
) ([]*discovery.Resource, []string, model.XdsLogDetails, bool) {
	// this agent does not support or is requesting to not receive true Delta NDS.
	if !bool(proxy.Metadata.DeltaNDS) {
		resources, logs := configgen.BuildNameTable(proxy, updates.Push)
		return resources, nil, logs, false
	}

	config := dnsServer.Config{
		Node:                        proxy,
		Push:                        updates.Push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	}

	// at least one config kind has changed that doesn't support delta, we need to rebuild the whole table and send
	// each table entry
	if !shouldUseNdsDelta(updates) {
		nt := dnsServer.BuildNameTable(config)
		return toPerHostResources(nt.GetTable()), nil, model.DefaultXdsLogDetails, false
	}

	headlessServiceChanged := sets.New[string]()
	deletedResources := sets.New[string]()
	var changed []*model.Service
	seen := sets.New[host.Name]()
	for key := range updates.ConfigsUpdated {
		hostname := host.Name(key.Name)
		var prevService *model.Service
		if proxy.PrevSidecarScope != nil {
			prevService = proxy.PrevSidecarScope.GetService(hostname)
		}
		service := proxy.SidecarScope.GetService(hostname)

		// we need to check if the previous service was headless to cleanup per-pod hostnames,
		// we also need to check current service because DNSName kind updates
		// don't trigger a SidecarScope recomputation.
		if isHeadless(prevService) || isHeadless(service) {
			headlessServiceChanged.Insert(key.Name)
		}

		if service == nil {
			deletedResources.Insert(key.Name)
			continue
		}

		// Multiple ConfigKeys (e.g. a ServiceEntry and a DNSName) can resolve to the same service;
		// dedup so BuildNameTableForServices doesn't process a hostname (and merge its IPs) twice.
		if seen.InsertContains(hostname) {
			continue
		}
		changed = append(changed, service)
	}

	// True delta: build only the changed services' records.
	newTable := dnsServer.BuildNameTableForServices(config, changed).GetTable()

	var removed []string
	for name := range watched.ResourceNames {
		if deletedResources.Contains(name) {
			removed = append(removed, name)
			continue
		}
		// check changed headless services for per-pod headless records that are no longer present in the new table
		if _, stillPresent := newTable[name]; !stillPresent && ownedByAny(name, headlessServiceChanged) {
			removed = append(removed, name)
		}
	}
	return toPerHostResources(newTable), removed, model.XdsLogDetails{Incremental: true}, true
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

// isHeadless reports whether a service publishes per-pod headless DNS records
// (<pod>.<subdomain>.<ns>.svc.<domain>). Only Kubernetes headless (Passthrough) services do.
// provider.Mock is accepted so the in-memory registry used in tests, which stamps Mock, exercises
// the same path as a real Kubernetes headless service.
func isHeadless(svc *model.Service) bool {
	if svc == nil || svc.Resolution != model.Passthrough {
		return false
	}
	// we also include provider.Mock because MemRegistry.AddService hardcodes it
	return svc.Attributes.ServiceRegistry == provider.Kubernetes || svc.Attributes.ServiceRegistry == provider.Mock
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
// Each resource carries the hostname in discovery.Resource.Name and the resolution attributes
// in the single-valued NameTable.NameInfo field (rather than a single-entry table map), avoiding
// duplicating the hostname and allocating a one-entry map per resource.
func toPerHostResources(table map[string]*dnsProto.NameTable_NameInfo) []*discovery.Resource {
	res := make([]*discovery.Resource, 0, len(table))
	for name, ni := range table {
		nt := &dnsProto.NameTable{NameInfo: ni}
		res = append(res, &discovery.Resource{
			Name:     name,
			Resource: protoconv.MessageToAny(nt),
		})
	}
	return res
}
