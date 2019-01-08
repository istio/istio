// Copyright 2019 Istio Authors
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

package pilot

const (

	// ConfigNamespace metadata defines the namespace where this proxy resides for the purposes of
	// network scoping.
	ConfigNamespace = "CONFIG_NAMESPACE"

	// InterceptionMode metadata defines the mode used to redirect inbound connections to Envoy.
	// This setting has no effect on outbound traffic: iptables REDIRECT is always used for
	// outbound connections.
	//   "REDIRECT" is the default, use iptables REDIRECT to NAT and redirect to Envoy.
	//   "TPROXY" uses iptables TPROXY to redirect to Envoy.
	InterceptionMode = "INTERCEPTION_MODE"

	// Network metadata defines the network the node belongs to. It is an optional metadata,
	// set at injection time. When set, the Endpoints returned to a note and not on same network
	// will be replaced with the gateway defined in the settings.
	Network = "NETWORK"

	// ProxyVersion metadata indicates the proxy's version, for version-specific configs.
	ProxyVersion = "ISTIO_PROXY_VERSION"

	// RouterMode metadata decides the behavior of Istio Gateway (normal or sni-dnat).
	//   "standard" is the default, indicates the normal gateway mode.
	//   "sni-dnat" is used for bridging two networks.
	RouterMode = "ROUTER_MODE"

	// RequestedNetworkView metadata defines the networks that the proxy requested. If set, Pilot
	// will only send endpoints corresponding to the networks that the proxy wants to see.
	// By default its set to "" so that the proxy sees endpoints from the default unnamed network.
	RequestedNetworkView = "REQUESTED_NETWORK_VIEW"

	// UserSDS metadata decides whether to enable ingress gateway agent.
	UserSDS = "USER_SDS"

	// InstanceIPs metadata holds the IP addresses of the proxy used to identify it and its
	// co-located service instances. In some cases, the host where the proxy and service instances
	// reside may have more than one IP address. Example: "10.3.3.3,10.4.4.4".
	InstanceIPs = "ISTIO_META_INSTANCE_IPS"
)
