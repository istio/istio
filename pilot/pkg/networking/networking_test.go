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

package networking

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
)

func TestModelProtocolToListenerProtocol(t *testing.T) {
	tests := []struct {
		name                       string
		protocol                   protocol.Instance
		direction                  core.TrafficDirection
		sniffingEnabledForInbound  bool
		sniffingEnabledForOutbound bool
		want                       ListenerProtocol
	}{
		{
			"TCP to TCP",
			protocol.TCP,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolTCP,
		},
		{
			"HTTP to HTTP",
			protocol.HTTP,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolHTTP,
		},
		{
			"HTTP to HTTP",
			protocol.HTTP_PROXY,
			core.TrafficDirection_OUTBOUND,
			true,
			true,
			ListenerProtocolHTTP,
		},
		{
			"MySQL to TCP",
			protocol.MySQL,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolTCP,
		},
		{
			"Inbound unknown to Auto",
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolAuto,
		},
		{
			"Outbound unknown to Auto",
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			true,
			true,
			ListenerProtocolAuto,
		},
		{
			"Inbound unknown to TCP",
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			false,
			true,
			ListenerProtocolTCP,
		},
		{
			"Outbound unknown to Auto (disable sniffing for inbound)",
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			false,
			true,
			ListenerProtocolAuto,
		},
		{
			"Inbound unknown to Auto (disable sniffing for outbound)",
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			true,
			false,
			ListenerProtocolAuto,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test.SetBoolForTest(t, &features.EnableProtocolSniffingForOutbound, tt.sniffingEnabledForOutbound)
			test.SetBoolForTest(t, &features.EnableProtocolSniffingForInbound, tt.sniffingEnabledForInbound)
			if got := ModelProtocolToListenerProtocol(tt.protocol, tt.direction); got != tt.want {
				t.Errorf("ModelProtocolToListenerProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}
