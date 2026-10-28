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
	"strings"
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
)

// A cross-network inner tunnel forwarded by the ambient east-west gateway carries the service
// hostname as its CONNECT authority. With the bridge enabled, the sidecar's main_internal must use
// a matcher that routes <hostname>:<port> to a copy of that port's chain which sets the original
// destination, while keeping address-based selection for everything else. With the bridge
// disabled main_internal keeps filter_chain_match.
func TestSidecarHBONEServiceAddressedConnect(t *testing.T) {
	test.SetForTest(t, &features.EnableSidecarHBONEListening, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)

	httpSvc := buildServiceWithPort("echo.default.svc.cluster.local", 80, protocol.HTTP, tnow)
	httpInst := buildServiceInstance(httpSvc, "1.1.1.1")
	httpInst.Endpoint.EndpointPort = 8080
	// A port without a declared protocol is sniffed: one plain and one HTTP chain.
	autoSvc := buildServiceWithPort("auto.default.svc.cluster.local", 70, protocol.Unsupported, tnow)
	autoInst := buildServiceInstance(autoSvc, "1.1.1.1")
	autoInst.Endpoint.EndpointPort = 7070

	for _, enabled := range []bool{false, true} {
		name := "bridge disabled"
		if enabled {
			name = "bridge enabled"
		}
		t.Run(name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSidecarAmbientBridge, enabled)
			cg := NewConfigGenTest(t, TestOptions{
				Services:  []*model.Service{httpSvc, autoSvc},
				Instances: []*model.ServiceInstance{httpInst, autoInst},
			})
			p := getProxy()
			p.Metadata.EnableHBONE = true
			proxy := cg.SetupProxy(p)

			var internal *listener.Listener
			for _, l := range cg.Listeners(proxy) {
				if l.Name == MainInternalName {
					internal = l
				}
			}
			if internal == nil {
				t.Fatalf("no %s listener", MainInternalName)
			}
			if !enabled {
				if internal.FilterChainMatcher != nil {
					t.Fatalf("main_internal should keep filter_chain_match when the bridge is disabled")
				}
				return
			}

			if internal.FilterChainMatcher == nil {
				t.Fatalf("main_internal should use a filter chain matcher when the bridge is enabled")
			}
			tree := internal.FilterChainMatcher.String()
			for _, want := range []string{
				"echo.default.svc.cluster.local:80", // authority branch
				"auto.default.svc.cluster.local:70",
				"1.1.1.1",          // destination IP branch
				`"8080"`, `"7070"`, // destination ports
				"'http/1.1'", // sniffing on 7070
			} {
				if !strings.Contains(tree, want) {
					t.Errorf("matcher should contain %s", want)
				}
			}

			names := sets.New[string]()
			chains := map[string]*listener.FilterChain{}
			for _, c := range internal.FilterChains {
				if c.FilterChainMatch != nil {
					t.Errorf("chain %s still has filter_chain_match", c.Name)
				}
				if names.InsertContains(c.Name) {
					t.Errorf("duplicate chain name %s", c.Name)
				}
				chains[c.Name] = c
			}
			for name, dst := range map[string]string{
				"0.0.0.0_8080-authority":      "1.1.1.1:8080",
				"0.0.0.0_7070-authority":      "1.1.1.1:7070",
				"0.0.0.0_7070-http-authority": "1.1.1.1:7070",
			} {
				c := chains[name]
				if c == nil {
					t.Errorf("missing authority chain %s; have %v", name, sets.SortedList(names))
					continue
				}
				if c.Filters[0].Name != "set_dst_address" || !strings.Contains(c.Filters[0].String(), dst) {
					t.Errorf("chain %s should first set the original destination to %s", name, dst)
				}
			}
			for _, name := range []string{"0.0.0.0_8080", "0.0.0.0_7070", "0.0.0.0_7070-http"} {
				if c := chains[name]; c == nil || c.Filters[0].Name == "set_dst_address" {
					t.Errorf("address-selected chain %s should exist unchanged", name)
				}
			}
		})
	}
}
