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

package v1alpha3

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	dnstable "github.com/envoyproxy/go-control-plane/envoy/data/dns/v3"
	dnsfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/dns_filter/v3alpha"
	stringmatcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/proxy/envoy/filters"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
)

var knownSuffixes = []*stringmatcher.StringMatcher{
	{
		MatchPattern: &stringmatcher.StringMatcher_SafeRegex{
			SafeRegex: &stringmatcher.RegexMatcher{
				EngineType: &stringmatcher.RegexMatcher_GoogleRe2{GoogleRe2: &stringmatcher.RegexMatcher_GoogleRE2{}},
				Regex:      ".*", // Match everything.. All DNS queries go through Envoy. Unknown ones will be forwarded
			},
		},
	},
}
// This is a UDP listener is on port 15013 (TODO customize me via mesh config).
// It has a DNS listener filter containing the cluster IPs of all services visible to the proxy (those that have one anyway).
// The list of service cluster IPs will aid VMs, and multi-cluster setups to resolve services that may not exist in the local
// cluster.
//
// TODO: Expand the logic to include necessary IPs for service entries that have NONE resolution with wildcard hosts
// like *.example.com, or those that have service entries for TCP services with DNS resolution, without cluster IP.
// In these cases, just allocate some dummy IP from
// 127.255.0.0/16 subnet. But make sure that we are also building a TCP listener on this port to process this
// traffic appropriately. These IPs need not be consistent across all proxies in the mesh because DNS resolution is local to the pod/VM.
// They need to be consistent between the IP configured in the DNS resolver here and the IP configured in the listeners
// sent to this proxy. Once this system works properly, we should get rid of all k8s dns hacks
func (configgen *ConfigGeneratorImpl) buildSidecarDNSListener(node *model.Proxy, push *model.PushContext) *listener.Listener {
	// We will ship the DNS filter to all 1.7+ proxies if dns capture is enabled in the proxy.
	if node.Metadata.DnsCapture == "" {
		return nil
	}

	_, localhost := getActualWildcardAndLocalHost(node)
	address := util.BuildAddress(localhost, model.SidecarDNSListenerPort)
	// Convert the address to a UDP address
	address.GetSocketAddress().Protocol = core.SocketAddress_UDP

	inlineDNSTable := configgen.buildInlineDNSTable(node, push)

	dnsFilterConfig := &dnsfilter.DnsFilterConfig{
		StatPrefix: "dns",
		ServerConfig: &dnsfilter.DnsFilterConfig_ServerContextConfig{
			ConfigSource: &dnsfilter.DnsFilterConfig_ServerContextConfig_InlineDnsTable{InlineDnsTable: inlineDNSTable},
		},
	}
	dnsFilter := &listener.ListenerFilter{
		Name: filters.DNSListenerFilterName,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(dnsFilterConfig),
		},
	}

	return &listener.Listener{
		Name:             dnsListenerName,
		Address:          address,
		ListenerFilters:  []*listener.ListenerFilter{dnsFilter},
		TrafficDirection: core.TrafficDirection_OUTBOUND,
	}
}

func (configgen *ConfigGeneratorImpl) buildInlineDNSTable(node *model.Proxy, push *model.PushContext) *dnstable.DnsTable {

	// build a virtual domain for each service visible to this sidecar
	virtualDomains := make([]*dnstable.DnsTable_DnsVirtualDomain, 0)

	for _, svc := range push.Services(node) {
		// we cannot take services with wildcards in the address field as DNS resolution wont work (for now)
		if svc.Hostname.IsWildCarded() {
			continue
		}
		address := svc.GetServiceAddressForProxy(node)
		if address == constants.UnspecifiedIP {
			continue
		}

		virtualDomains = append(virtualDomains, &dnstable.DnsTable_DnsVirtualDomain{
			Name: string(svc.Hostname),
			Endpoint: &dnstable.DnsTable_DnsEndpoint{
				EndpointConfig: &dnstable.DnsTable_DnsEndpoint_AddressList{
					AddressList: &dnstable.DnsTable_AddressList{Address: []string{address}},
				},
			},
		})
	}

	return &dnstable.DnsTable{
		VirtualDomains: virtualDomains,
		KnownSuffixes:  knownSuffixes,
	}
}
