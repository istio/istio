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

package tunnelingconfig

import (
	"net"
	"strconv"

	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"

	networking "istio.io/api/networking/v1alpha3"
)

type ApplyFunc = func(tcpProxy *tcp.TcpProxy, destinationRule *networking.DestinationRule, subsetName string)

// Apply configures tunneling_config in a given TcpProxy depending on the destination rule and the destination hosts
var Apply ApplyFunc = func(tcpProxy *tcp.TcpProxy, destinationRule *networking.DestinationRule, subsetName string) {
	var tunnelSettings *networking.TrafficPolicy_TunnelSettings
	if subsetName != "" {
		for _, s := range destinationRule.GetSubsets() {
			if s.Name == subsetName {
				tunnelSettings = s.GetTrafficPolicy().GetTunnel()
				break
			}
		}
	} else {
		tunnelSettings = destinationRule.GetTrafficPolicy().GetTunnel()
	}

	if tunnelSettings == nil {
		return
	}

	tcpProxy.TunnelingConfig = &tcp.TcpProxy_TunnelingConfig{
		Hostname: net.JoinHostPort(tunnelSettings.GetTargetHost(), strconv.Itoa(int(tunnelSettings.GetTargetPort()))),
		UsePost:  tunnelSettings.Protocol == "POST",
	}
}

// Skip has no effect; its only purpose is to avoid passing nil values for ApplyFunc arguments
// when it is not desired to apply `tunneling_config` to a listener, e.g. AUTO_PASSTHROUGH
var Skip ApplyFunc = func(_ *tcp.TcpProxy, _ *networking.DestinationRule, _ string) {}
