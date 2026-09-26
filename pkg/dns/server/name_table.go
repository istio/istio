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

package server

import (
	"strings"

	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	dnsutil "istio.io/istio/pkg/dns"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/slices"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

// Config for building the name table.
type Config struct {
	Node *model.Proxy
	Push *model.PushContext

	// MulticlusterHeadlessEnabled if true, the DNS name table for a headless service will resolve to
	// same-network endpoints in any cluster.
	MulticlusterHeadlessEnabled bool
}

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy
func BuildNameTable(cfg Config) *dnsProto.NameTable {
	out := &dnsProto.NameTable{
		Table: make(map[string]*dnsProto.NameTable_NameInfo),
	}
	for _, el := range cfg.Node.SidecarScope.EgressListeners {
		for _, svc := range el.Services() {
			addServiceToTable(out, cfg, svc)
		}
	}
	return out
}

func buildNameTableForServices(cfg Config, services []*model.Service) *dnsProto.NameTable {
	out := &dnsProto.NameTable{Table: make(map[string]*dnsProto.NameTable_NameInfo, len(services))}
	for _, svc := range services {
		addServiceToTable(out, cfg, svc)
	}
	return out
}

func addServiceToTable(out *dnsProto.NameTable, cfg Config, svc *model.Service) {
	var addressList []string
	hostName := svc.Hostname
	headless := false
	for _, svcAddress := range svc.GetAllAddressesForProxy(cfg.Node) {
		if svcAddress == constants.UnspecifiedIP {
			headless = true
			break
		}
		// Filter out things we cannot parse as IP. Generally this means CIDRs, as anything else
		// should be caught in validation.
		if !netutil.IsValidIPAddress(svcAddress) {
			continue
		}
		addressList = append(addressList, svcAddress)
	}
	if headless {
		// The IP will be unspecified here if its headless service or if the auto
		// IP allocation logic for service entry was unable to allocate an IP.
		if svc.Resolution == model.Passthrough && len(svc.Ports) > 0 {
			localAddresses := make(map[string][]string)
			remoteAddresses := make(map[string][]string)
			hostMetadata := make(map[string]types.NamespacedName)
			// Iterate all ports to collect endpoints from every EndpointSlice.
			// Dedup by address since pod IPs are unique (IPAM guarantee).
			seen := sets.New[string]()
			for _, endpoints := range cfg.Push.ServiceEndpoints(svc.Key()) {
				for _, instance := range endpoints {
					isValidInstance := true
					for _, addr := range instance.Addresses {
						if !netutil.IsValidIPAddress(addr) {
							isValidInstance = false
							break
						}
					}
					if len(instance.Addresses) == 0 || !isValidInstance ||
						(!svc.Attributes.PublishNotReadyAddresses && instance.HealthStatus != model.Healthy) {
						continue
					}
					if seen.InsertContains(instance.FirstAddressOrNil()) {
						continue
					}
					// TODO(stevenctl): headless across-networks https://github.com/istio/istio/issues/38327
					sameNetwork := cfg.Node.InNetwork(instance.Network)
					sameCluster := cfg.Node.InCluster(instance.Locality.ClusterID)
					// For all k8s headless services, populate the dns table with the endpoint IPs as k8s does.
					// And for each individual pod, populate the dns table with the endpoint IP with a manufactured host name.
					if instance.SubDomain != "" && sameNetwork {
						// Follow k8s pods dns naming convention of "<hostname>.<subdomain>.<pod namespace>.svc.<cluster domain>"
						// i.e. "mysql-0.mysql.default.svc.cluster.local".
						parts := strings.SplitN(hostName.String(), ".", 2)
						if len(parts) != 2 {
							continue
						}
						shortName := instance.HostName + "." + instance.SubDomain
						host := shortName + "." + parts[1] // Add cluster domain.
						hostMetadata[host] = types.NamespacedName{Name: shortName, Namespace: svc.Attributes.Namespace}
						if sameCluster {
							localAddresses[host] = append(localAddresses[host], instance.Addresses...)
						} else {
							remoteAddresses[host] = append(remoteAddresses[host], instance.Addresses...)
						}
					}
					skipForMulticluster := !cfg.MulticlusterHeadlessEnabled && !sameCluster
					if skipForMulticluster || !sameNetwork {
						// We take only cluster-local endpoints. While this seems contradictory to
						// our logic other parts of the code, where cross-cluster is the default.
						// However, this only impacts the DNS response. If we were to send all
						// endpoints, cross network routing would break, as we do passthrough LB and
						// don't go through the network gateway. While we could, hypothetically, send
						// "network-local" endpoints, this would still make enabling DNS give vastly
						// different load balancing than without, so its probably best to filter.
						// This ends up matching the behavior of Kubernetes DNS.
						continue
					}
					addressList = append(addressList, instance.Addresses...)
				}
			}
			// Write local cluster entries first
			for host, ips := range localAddresses {
				meta := hostMetadata[host]
				out.Table[host] = &dnsProto.NameTable_NameInfo{
					Ips:       ips,
					Registry:  string(svc.Attributes.ServiceRegistry),
					Namespace: meta.Namespace,
					Shortname: meta.Name,
				}
			}
			// Write remote cluster entries only if local doesn't exist
			for host, ips := range remoteAddresses {
				if _, exists := localAddresses[host]; !exists {
					meta := hostMetadata[host]
					out.Table[host] = &dnsProto.NameTable_NameInfo{
						Ips:       ips,
						Registry:  string(svc.Attributes.ServiceRegistry),
						Namespace: meta.Namespace,
						Shortname: meta.Name,
					}
				}
			}
		}
	}
	if len(addressList) == 0 {
		// could not reliably determine the addresses of endpoints of headless service
		// or this is not a k8s service
		return
	}

	if ni, f := out.Table[hostName.String()]; !f {
		nameInfo := &dnsProto.NameTable_NameInfo{
			Ips:      addressList,
			Registry: string(svc.Attributes.ServiceRegistry),
		}
		if svc.Attributes.ServiceRegistry == provider.Kubernetes &&
			!strings.HasSuffix(hostName.String(), "."+constants.DefaultClusterSetLocalDomain) {
			// The agent will take care of resolving a, a.ns, a.ns.svc, etc.
			// No need to provide a DNS entry for each variant.
			//
			// NOTE: This is not done for Kubernetes Multi-Cluster Services (MCS) hosts, in order
			// to avoid conflicting with the entries for the regular (cluster.local) service.
			nameInfo.Namespace = svc.Attributes.Namespace
			nameInfo.Shortname = svc.Attributes.Name
		}
		out.Table[hostName.String()] = nameInfo
	} else if provider.ID(ni.Registry) != provider.Kubernetes {
		// 2 possible cases:
		// 1. If the SE has multiple addresses(vips) specified, merge the ips
		// 2. If the previous SE is a decorator of the k8s service, give precedence to the k8s service
		if svc.Attributes.ServiceRegistry == provider.Kubernetes {
			ni.Ips = addressList
			ni.Registry = string(provider.Kubernetes)
			if !strings.HasSuffix(hostName.String(), "."+constants.DefaultClusterSetLocalDomain) {
				ni.Namespace = svc.Attributes.Namespace
				ni.Shortname = svc.Attributes.Name
			}
		} else {
			ni.Ips = append(ni.Ips, addressList...)
		}
	}
}

type exactNameCandidate struct {
	info   *dnsProto.NameTable_NameInfo
	exact  bool
	source string
	owner  string
}

// DeltaNameTable retains the contributors to each materialized DNS name for one NDS stream.
// It is independent of the legacy NameTable representation.
// byService and servicesByName index owners; byName is the reverse contributor index.
// addService and removeService must update all three together.
type DeltaNameTable struct {
	byService      map[string]sets.String
	byName         map[string]*deltaNameState
	servicesByName map[string][]*model.Service
}

type deltaNameState struct {
	contributors map[string]exactNameCandidate
	winner       exactNameCandidate
	hasWinner    bool
}

// NewDeltaNameTable builds the initial materialized view for a Delta NDS stream.
func NewDeltaNameTable(cfg Config) *DeltaNameTable {
	state := &DeltaNameTable{
		byService:      make(map[string]sets.String),
		byName:         make(map[string]*deltaNameState),
		servicesByName: make(map[string][]*model.Service),
	}
	for owner, services := range serviceGroups(cfg, nil) {
		state.addService(cfg, owner, services)
	}
	for _, name := range state.byName {
		name.winner, name.hasWinner = preferredName(name.contributors)
	}
	return state
}

// Table returns the current materialized DNS resources. Entries are immutable while in the state.
func (d *DeltaNameTable) Table() map[string]*dnsProto.NameTable_NameInfo {
	table := make(map[string]*dnsProto.NameTable_NameInfo, len(d.byName))
	for name, state := range d.byName {
		if state.hasWinner {
			table[name] = state.winner.info
		}
	}
	return table
}

// Removed returns names present in previous but absent from d.
func (d *DeltaNameTable) Removed(previous *DeltaNameTable) []string {
	if previous == nil {
		return nil
	}
	removed := make([]string, 0)
	for name := range previous.byName {
		if _, found := d.byName[name]; !found {
			removed = append(removed, name)
		}
	}
	slices.Sort(removed)
	return removed
}

// Update replaces changed contributions and returns only effective changes. Service-definition
// updates reload their complete same-host group; endpoint-only updates reuse the retained group.
func (d *DeltaNameTable) Update(cfg Config, hostnames, reloadServices sets.String) (map[string]*dnsProto.NameTable_NameInfo, []string) {
	reloadServices = normalizedNames(reloadServices)
	var currentServices map[string][]*model.Service
	if len(reloadServices) > 0 {
		currentServices = serviceGroups(cfg, reloadServices)
	}
	touched := sets.New[string]()
	// TODO(rudrakhp): Track endpoint-level contributions so endpoint churn rebuilds only changed names within a service.
	for hostname := range hostnames {
		owner := normalizeName(hostname)
		services := d.servicesByName[owner]
		if reloadServices.Contains(owner) {
			services = currentServices[owner]
		}
		touched.Merge(d.removeService(owner))
		if len(services) > 0 {
			touched.Merge(d.addService(cfg, owner, services))
		}
	}

	changed := make(map[string]*dnsProto.NameTable_NameInfo)
	removed := make([]string, 0)
	for name := range touched {
		state := d.byName[name]
		previous, hadPrevious := state.winner, state.hasWinner
		current, found := preferredName(state.contributors)
		switch {
		case !found:
			delete(d.byName, name)
			if hadPrevious {
				removed = append(removed, name)
			}
		default:
			state.winner, state.hasWinner = current, true
			if !hadPrevious || !proto.Equal(previous.info, current.info) {
				changed[name] = current.info
			}
		}
	}
	slices.Sort(removed)
	return changed, removed
}

func normalizedNames(names sets.String) sets.String {
	out := sets.NewWithLength[string](len(names))
	for name := range names {
		out.Insert(normalizeName(name))
	}
	return out
}

func (d *DeltaNameTable) removeService(owner string) sets.String {
	touched := sets.New[string]()
	for name := range d.byService[owner] {
		touched.Insert(name)
		delete(d.byName[name].contributors, owner)
	}
	delete(d.byService, owner)
	delete(d.servicesByName, owner)
	return touched
}

func (d *DeltaNameTable) addService(cfg Config, owner string, services []*model.Service) sets.String {
	contributions := exactNameContributions(cfg, services)
	touched := sets.NewWithLength[string](len(contributions))
	for name, candidate := range contributions {
		candidate.owner = owner
		touched.Insert(name)
		state := d.byName[name]
		if state == nil {
			state = &deltaNameState{contributors: make(map[string]exactNameCandidate)}
			d.byName[name] = state
		}
		state.contributors[owner] = candidate
	}
	d.byService[owner] = touched
	d.servicesByName[owner] = services
	return touched
}

func serviceGroups(cfg Config, only sets.String) map[string][]*model.Service {
	groups := make(map[string][]*model.Service)
	for _, listener := range cfg.Node.SidecarScope.EgressListeners {
		for _, service := range listener.Services() {
			owner := serviceOwner(service)
			if owner == "" || (len(only) > 0 && !only.Contains(owner)) {
				continue
			}
			groups[owner] = append(groups[owner], service)
		}
	}
	return groups
}

func exactNameContributions(cfg Config, services []*model.Service) map[string]exactNameCandidate {
	return exactNameCandidates(cfg, buildNameTableForServices(cfg, services))
}

func exactNameCandidates(cfg Config, canonical *dnsProto.NameTable) map[string]exactNameCandidate {
	out := make(map[string]exactNameCandidate)
	for source, info := range canonical.GetTable() {
		if info == nil {
			continue
		}
		source = normalizeName(source)
		canonicalInfo := canonicalNameInfo(info)
		for name := range namesForEntry(cfg.Node, source, info) {
			name = normalizeName(name)
			candidate := exactNameCandidate{
				info:   canonicalInfo,
				exact:  name == source,
				source: source,
				owner:  source,
			}
			if current, found := out[name]; !found || preferCandidate(candidate, current) {
				out[name] = candidate
			}
		}
	}
	return out
}

func preferredName(candidates map[string]exactNameCandidate) (exactNameCandidate, bool) {
	var preferred exactNameCandidate
	found := false
	for _, candidate := range candidates {
		if !found || preferCandidate(candidate, preferred) {
			preferred = candidate
			found = true
		}
	}
	return preferred, found
}

func serviceOwner(service *model.Service) string {
	if service == nil {
		return ""
	}
	return normalizeName(service.Hostname.String())
}

func canonicalNameInfo(in *dnsProto.NameTable_NameInfo) *dnsProto.NameTable_NameInfo {
	out := cloneNameInfo(in)
	slices.Sort(out.Ips)
	return out
}

// ExpandNameTable materializes the semantic aliases from a canonical NameTable and resolves
// collisions deterministically. If only is non-empty, only those final names are returned.
func ExpandNameTable(cfg Config, canonical *dnsProto.NameTable, only sets.String) *dnsProto.NameTable {
	candidates := exactNameCandidates(cfg, canonical)
	out := &dnsProto.NameTable{Table: make(map[string]*dnsProto.NameTable_NameInfo, len(candidates))}
	for name, candidate := range candidates {
		if len(only) > 0 && !only.Contains(name) {
			continue
		}
		out.Table[name] = candidate.info
	}
	return out
}

func namesForEntry(node *model.Proxy, hostname string, info *dnsProto.NameTable_NameInfo) sets.String {
	hostname = normalizeName(hostname)
	out := sets.New(hostname)
	if info.Registry != string(provider.Kubernetes) || info.Shortname == "" || info.Namespace == "" {
		return out
	}

	proxyNamespace := model.GetProxyConfigNamespace(node)
	parts := strings.Split(strings.TrimSuffix(node.DNSDomain, "."), ".")
	if len(parts) > 0 && parts[0] == proxyNamespace {
		parts = parts[1:]
	}
	proxyDomain := strings.Join(parts, ".")
	for alias := range dnsutil.GenerateAltHosts(hostname, info, proxyNamespace, proxyDomain, parts) {
		out.Insert(normalizeName(strings.TrimSuffix(alias, ".")))
	}
	return out
}

func preferCandidate(candidate, current exactNameCandidate) bool {
	if candidate.exact != current.exact {
		return candidate.exact
	}
	candidateKube := candidate.info.Registry == string(provider.Kubernetes)
	currentKube := current.info.Registry == string(provider.Kubernetes)
	if candidateKube != currentKube {
		return candidateKube
	}
	if candidate.source != current.source {
		return candidate.source < current.source
	}
	return candidate.owner < current.owner
}

func cloneNameInfo(in *dnsProto.NameTable_NameInfo) *dnsProto.NameTable_NameInfo {
	return &dnsProto.NameTable_NameInfo{
		Ips:       slices.Clone(in.Ips),
		Registry:  in.Registry,
		Shortname: in.Shortname,
		Namespace: in.Namespace,
		AltHosts:  slices.Clone(in.GetAltHosts()), //nolint:staticcheck
	}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}
