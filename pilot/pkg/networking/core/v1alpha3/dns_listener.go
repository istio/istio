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

// This is a UDP listener is on port 15013.  It has a DNS listener filter containing the
// cluster IPs of all services visible to the proxy (those that have one anyway).  The
// list of service cluster IPs will aid VMs, and multi-cluster setups to resolve services
// that may not exist in the local cluster. In addition, we also add the 'address' field
// from all the service entries (user provided or auto allocated).  Since all callers use
// GetServiceAddressForProxy(), we can be sure that when sending DNS filter to a proxy
// with dns capture enabled, the auto allocated IPs we send for DNS will be the same as
// the ones used for building the listeners for those (TCP) services.
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
		// we cannot take services with wildcards in the address field. The reason
		// is that even if we provide some dummy IP (subject to enabling this
		// feature in Envoy), after capturing the traffic from the app, the
		// sidecar would need to forward to the real IP. But to determine the real
		// IP, the sidecar would have to know the non-wildcard FQDN that the
		// application was trying to resolve. This information is not available
		// for TCP services. The wildcard hostname is not a problem for HTTP
		// services though, as we usually setup a listener on 0.0.0.0, process
		// based on http virtual host and forward to the orig destination IP.
		//
		// Long story short, if the user has a TCP service of the form
		//
		// host: *.mysql.aws.com, port 3306,
		//
		// our only recourse is to allocate a 0.0.0.0:3306 listener and forward to
		// original dest IP. It is now the user's responsibility to not allocate
		// another wildcard service on the same port. i.e.
		//
		// 1. host: *.mysql.aws.com, port 3306
		// 2. host: *.mongo.aws.com, port 3306 will result in conflict. Traffic may still
		// flow but metrics wont be correct.
		if svc.Hostname.IsWildCarded() {
			continue
		}
		address := svc.GetServiceAddressForProxy(node)

		// The IP will be unspecified here if its headless service or if the auto
		// IP allocation logic for service entry was unable to allocate an IP.
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
