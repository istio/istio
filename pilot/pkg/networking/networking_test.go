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
			"Outbound unknown to Auto (disable sniffing for outbound)",
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			true,
			false,
			ListenerProtocolTCP,
		},
		{
			"Inbound unknown to Auto (disable sniffing for outbound)",
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			true,
			false,
			ListenerProtocolAuto,
		},
		{
			"UDP to UDP",
			protocol.UDP,
			core.TrafficDirection_INBOUND,
			true,
			false,
			ListenerProtocolUnknown,
		},
		{
			"Unknown Protocol",
			"Bad Protocol",
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

func TestMakeTunnelAbility(t *testing.T) {
	tests := []struct {
		name  string
		value TunnelType
		want  TunnelAbility
	}{
		{
			"test TunnelAbility method for H2Tunnel",
			H2Tunnel,
			1,
		},
		{
			"test TunnelAbility method for NoTunnel",
			NoTunnel,
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MakeTunnelAbility(tt.value); got != tt.want {
				t.Errorf("Failed to get MakeTunnelAbility: got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name  string
		value TunnelType
		want  string
	}{
		{
			"test ToString method for H2Tunnel",
			H2Tunnel,
			"H2Tunnel",
		},
		{
			"test ToString method for NoTunnel",
			NoTunnel,
			"notunnel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.ToString(); got != tt.want {
				t.Errorf("Failed to get ToString: got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportH2Tunnel(t *testing.T) {
	tests := []struct {
		name  string
		value TunnelType
		want  bool
	}{
		{
			"test SupportH2Tunnel method for NoTunnel",
			NoTunnel,
			false,
		},
		{
			"test SupportH2Tunnel method for H2Tunnel",
			H2Tunnel,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TunnelAbility(tt.value).SupportH2Tunnel(); got != tt.want {
				t.Errorf("Failed to get SupportH2Tunnel:: got = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestToEnvoySocketProtocol(t *testing.T) {
	tests := []struct {
		name  string
		value TunnelType
		want  core.SocketAddress_Protocol
	}{
		{
			"test ToEnvoySocketProtocol method for Notunnel",
			NoTunnel,
			0,
		},
		{
			"test ToEnvoySocketProtocol method for H2Tunnel",
			H2Tunnel,
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TransportProtocol(tt.value).ToEnvoySocketProtocol(); got != tt.want {
				t.Errorf("Failed to get ToEnvoySocketProtocol:: got = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		value uint
		want  string
	}{
		{
			"test String method for tcp transport protocol",
			TransportProtocolTCP,
			"tcp",
		},
		{
			"test String method for quic transport protocol",
			TransportProtocolQUIC,
			"quic",
		},
		{
			"test String method for invalid transport protocol",
			3,
			"unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TransportProtocol(tt.value).String(); got != tt.want {
				t.Errorf("Failed to get TransportProtocol.String :: got = %v, want %v", got, tt.want)
			}
		})
	}
}
