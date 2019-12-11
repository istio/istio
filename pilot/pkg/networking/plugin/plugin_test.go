// Copyright 2018 Istio Authors
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

package plugin

import (
	"os"
	"testing"

	"istio.io/istio/pilot/pkg/features"

	"istio.io/istio/pkg/config/protocol"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"istio.io/istio/pilot/pkg/model"
)

var (
	proxy = &model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			IstioVersion:    "1.4",
			ConfigNamespace: "not-default",
		},
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 4},
		ConfigNamespace: "not-default",
	}
)

func TestModelProtocolToListenerProtocol(t *testing.T) {
	tests := []struct {
		name                       string
		node                       *model.Proxy
		protocol                   protocol.Instance
		direction                  core.TrafficDirection
		sniffingEnabledForInbound  bool
		sniffingEnabledForOutbound bool
		want                       ListenerProtocol
	}{
		{
			"TCP to TCP",
			proxy,
			protocol.TCP,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolTCP,
		},
		{
			"HTTP to HTTP",
			proxy,
			protocol.HTTP,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolHTTP,
		},
		{
			"MySQL to TCP",
			proxy,
			protocol.MySQL,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolTCP,
		},
		{
			"Inbound unknown to Auto",
			proxy,
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			true,
			true,
			ListenerProtocolAuto,
		},
		{
			"Outbound unknown to Auto",
			proxy,
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			true,
			true,
			ListenerProtocolAuto,
		},
		{
			"Inbound unknown to TCP",
			proxy,
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			false,
			true,
			ListenerProtocolTCP,
		},
		{
			"Outbound unknown to Auto (disable sniffing for inbound)",
			proxy,
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			false,
			true,
			ListenerProtocolAuto,
		}, {
			"Inbound unknown to Auto (disable sniffing for outbound)",
			proxy,
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			true,
			false,
			ListenerProtocolAuto,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.sniffingEnabledForInbound {
				_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "true")
			} else {
				_ = os.Setenv(features.EnableProtocolSniffingForInbound.Name, "false")
			}
			if tt.sniffingEnabledForOutbound {
				_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "true")
			} else {
				_ = os.Setenv(features.EnableProtocolSniffingForOutbound.Name, "false")
			}

			if got := ModelProtocolToListenerProtocol(tt.node, tt.protocol, tt.direction); got != tt.want {
				t.Errorf("ModelProtocolToListenerProtocol() = %v, want %v", got, tt.want)
			}

			_ = os.Unsetenv(features.EnableProtocolSniffingForInbound.Name)
			_ = os.Unsetenv(features.EnableProtocolSniffingForOutbound.Name)
		})
	}
}
