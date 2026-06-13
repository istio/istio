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
	"testing"

	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/wellknown"
)

// TestInboundStatPrefixUniqueness verifies that no stat_prefix is shared across
// filter chains with different transport protocols on the virtualInbound listener.
// Under permissive mTLS, Istiod generates both TLS and plaintext filter chains that
// previously shared the same stat_prefix, causing gauge collisions in Envoy's stats.
func TestInboundStatPrefixUniqueness(t *testing.T) {
	services := []*model.Service{
		buildService("test.com", wildcardIPv4, protocol.HTTP, tnow),
		buildService("tcp.example.com", wildcardIPv4, protocol.TCP, tnow),
	}

	listeners := buildListeners(t, TestOptions{
		Services: services,
	}, nil)

	l := xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	if l == nil {
		t.Fatal("virtualInbound listener not found")
	}

	// Map each stat_prefix to the set of transport protocols that use it.
	seen := make(map[string]map[string]bool) // stat_prefix -> transport -> true
	for _, fc := range l.GetFilterChains() {
		transport := fc.GetFilterChainMatch().GetTransportProtocol()
		if transport == "" {
			transport = "unset"
		}

		var statPrefix string
		for _, f := range fc.GetFilters() {
			switch f.GetName() {
			case wellknown.HTTPConnectionManager:
				hcmConfig := &hcm.HttpConnectionManager{}
				if err := f.GetTypedConfig().UnmarshalTo(hcmConfig); err == nil {
					statPrefix = hcmConfig.GetStatPrefix()
				}
			case wellknown.TCPProxy:
				tcpConfig := &tcp.TcpProxy{}
				if err := f.GetTypedConfig().UnmarshalTo(tcpConfig); err == nil {
					statPrefix = tcpConfig.GetStatPrefix()
				}
			}
			if statPrefix != "" {
				break
			}
		}

		if statPrefix == "" {
			continue
		}

		if seen[statPrefix] == nil {
			seen[statPrefix] = make(map[string]bool)
		}
		seen[statPrefix][transport] = true
	}

	for prefix, transports := range seen {
		if len(transports) > 1 {
			t.Errorf("stat_prefix %q is shared across transport protocols %v; "+
				"this causes gauge collisions in Envoy's stats internals", prefix, transports)
		}
	}
}
