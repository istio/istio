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

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	dnsProto "istio.io/istio/pkg/dns/proto"
	netutil "istio.io/istio/pkg/util/net"
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
					for _, instance := range cfg.Push.ServiceEndpointsByPort(svc, svc.Ports[0].Port, nil) {
						// addresses may be empty or invalid here
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
						// TODO: should we skip the node's own IP like we do in listener?
						addressList = append(addressList, instance.Addresses...)
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
						if _, exists := out.Table[host]; !exists {
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
				continue
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
	}
	return out
}
