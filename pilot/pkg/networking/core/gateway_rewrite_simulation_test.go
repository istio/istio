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

package core_test

import (
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestGatewayAPIPrefixRewrite(t *testing.T) {
	for _, tt := range []struct {
		name          string
		MatchPath     string
		Replacement   string
		prefixRewrite string
	}{
		{
			name: "root prefix", MatchPath: "/", Replacement: "/bookinfo",
			prefixRewrite: "/bookinfo/",
		},
		{
			name: "root prefix with trailing replacement slash", MatchPath: "/", Replacement: "/bookinfo/",
			prefixRewrite: "/bookinfo/",
		},
		{
			name: "non-root prefix", MatchPath: "/store", Replacement: "/bookinfo",
			prefixRewrite: "/bookinfo",
		},
		{
			name: "non-root prefix with trailing slashes", MatchPath: "/store/", Replacement: "/bookinfo/",
			prefixRewrite: "/bookinfo",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesObjectString: tmpl.MustEvaluateFile("testdata/gateway-root-prefix-rewrite.yaml", tt),
			})
			proxy := s.SetupProxy(&model.Proxy{
				Type:        model.Router,
				IPAddresses: []string{"10.0.0.1"},
				Labels:      map[string]string{"istio": "ingressgateway"},
				Metadata: &model.NodeMetadata{
					Namespace: "default",
					Labels:    map[string]string{"istio": "ingressgateway"},
				},
			})
			sim := simulation.NewSimulation(t, s, proxy)
			for _, path := range []string{"/", "/productpage", "/static/style.css", "/api/v1/products/1?locale=en"} {
				sim.RunExpectations([]simulation.Expect{{
					Name: path,
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "bookinfo.demo",
						Path:       strings.TrimSuffix(tt.MatchPath, "/") + path,
						Protocol:   simulation.HTTP,
						CallMode:   simulation.CallModeGateway,
					},
					Result: simulation.Result{ClusterMatched: "outbound|9080||productpage.default.svc.cluster.local"},
				}})
			}
			xdstest.ValidateRouteConfigurations(t, sim.Routes)
			assert.Equal(t, len(sim.Routes), 1)
			assert.Equal(t, len(sim.Routes[0].VirtualHosts), 1)
			assert.Equal(t, len(sim.Routes[0].VirtualHosts[0].Routes), 1)
			action := sim.Routes[0].VirtualHosts[0].Routes[0].GetRoute()
			// Envoy replaces the matched prefix verbatim. A root match consumes the
			// separating slash, so it must also be present in the replacement.
			assert.Equal(t, action.GetPrefixRewrite(), tt.prefixRewrite)
		})
	}
}
