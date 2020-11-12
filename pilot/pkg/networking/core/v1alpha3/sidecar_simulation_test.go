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

package v1alpha3_test

import (
	"fmt"
	"sort"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/mesh"
)

func TestInbound(t *testing.T) {
	mtlsMode := func(m string) string {
		return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: %s
`, m)
	}
	svc := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - foo.bar
  endpoints:
  - address: 1.1.1.1
  location: MESH_INTERNAL
  resolution: STATIC
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: auto
    number: 81
---
`
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:   "disable",
		config: svc + mtlsMode("DISABLE"),
		calls: []simulation.Expect{
			{
				Name: "http inbound",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "auto port inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port2 inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP2,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto tcp inbound",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough http",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
			{
				Name: "passthrough tcp",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched: "InboundPassthroughClusterIpv4",
				},
			},
		},
	})

	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:   "permissive",
		config: svc + mtlsMode("PERMISSIVE"),
		calls: []simulation.Expect{
			{
				Name: "http port",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "http port tls",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// This is expected. Protocol is explicitly declared HTTP but we send TLS traffic
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "http port mtls",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|80",
					ClusterMatched:     "inbound|80||",
				},
			},
			{
				Name: "auto port port",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port port https",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Passed through as plain tcp
					ClusterMatched:     "inbound|81||",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port port https mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
					RouteMatched:       "default",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port tcp",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port tls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "auto port mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "istio",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "0.0.0.0_81",
					ClusterMatched:     "inbound|81||",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough http",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound-catchall-http",
				},
			},
			{
				Name: "passthrough tcp",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// TODO: This is a bug, see https://github.com/istio/istio/issues/26079#issuecomment-673699228
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough mtls",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Alpn:     "istio",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "virtualInbound",
					StrictMatch:        true,
				},
			},
			{
				Name: "passthrough https",
				Call: simulation.Call{
					Address:  "1.2.3.4",
					Port:     82,
					Alpn:     "http/1.1",
					Protocol: simulation.HTTP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					ListenerMatched:    "virtualInbound",
					FilterChainMatched: "virtualInbound-catchall-http",
					RouteMatched:       "default",
					VirtualHostMatched: "inbound|http|0",
					// TODO: This is a bug, see https://github.com/istio/istio/issues/26079#issuecomment-673699228
					// We should NOT be terminating TLs here, this is supposed to be passthrough. This breaks traffic
					// sending TLS with ALPN (ie curl, or many other clients) to a port not exposed in the service.
					StrictMatch: true,
				},
			},
		},
	})

	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		name:           "strict",
		config:         svc + mtlsMode("STRICT"),
		skipValidation: false,
		calls: []simulation.Expect{
			{
				Name: "http port",
				Call: simulation.Call{
					Port:     80,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "auto port http",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "auto port http mtls",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.HTTP,
					TLS:      simulation.MTLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					VirtualHostMatched: "inbound|http|81",
					ClusterMatched:     "inbound|81||",
				},
			},
			{
				Name: "auto port tcp",
				Call: simulation.Call{
					Port:     81,
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Plaintext to strict, should fail
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough plaintext",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					// Cannot send plaintext with strict
					Error: simulation.ErrNoFilterChain,
				},
			},
			{
				Name: "passthrough tls",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough mtls",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "istio",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					FilterChainMatched: "virtualInbound",
				},
			},
			{
				Name: "passthrough mtls http",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.TLS,
					Alpn:     "istio-http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					VirtualHostMatched: "inbound|http|0",
				},
			},
			{
				Name: "passthrough mtls http legacy",
				Call: simulation.Call{
					Port:     82,
					Address:  "1.2.3.4",
					Protocol: simulation.TCP,
					TLS:      simulation.MTLS,
					Alpn:     "http/1.1",
					CallMode: simulation.CallModeInbound,
				},
				Result: simulation.Result{
					ClusterMatched:     "InboundPassthroughClusterIpv4",
					VirtualHostMatched: "inbound|http|0",
				},
			},
		},
	})
}

func TestHeadlessServices(t *testing.T) {
	ports := `
  - name: http
    port: 80
  - name: auto
    port: 81
  - name: tcp
    port: 82
  - name: tls
    port: 83
  - name: https
    port: 84`

	calls := []simulation.Expect{}
	for _, call := range []simulation.Call{
		{Address: "1.2.3.4", Port: 80, Protocol: simulation.HTTP, HostHeader: "headless.default.svc.cluster.local"},

		// Auto port should support any protocol
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 81, Protocol: simulation.TCP, HostHeader: "headless.default.svc.cluster.local"},

		{Address: "1.2.3.4", Port: 82, Protocol: simulation.TCP, HostHeader: "headless.default.svc.cluster.local"},

		// TODO: https://github.com/istio/istio/issues/27677 use short host name
		{Address: "1.2.3.4", Port: 83, Protocol: simulation.TCP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
		{Address: "1.2.3.4", Port: 84, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "headless.default.svc.cluster.local"},
	} {
		calls = append(calls, simulation.Expect{
			Name: fmt.Sprintf("%s-%d", call.Protocol, call.Port),
			Call: call,
			Result: simulation.Result{
				ClusterMatched: fmt.Sprintf("outbound|%d||headless.default.svc.cluster.local", call.Port),
			},
		})
	}
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		kubeConfig: `apiVersion: v1
kind: Service
metadata:
  name: headless
  namespace: default
spec:
  clusterIP: None
  selector:
    app: headless
  ports:` + ports + `
---
apiVersion: v1
kind: Endpoints
metadata:
  name: headless
  namespace: default
subsets:
- addresses:
  - ip: 1.2.3.4
  ports:
` + ports,
		calls: calls,
	},
	)
}

func TestPassthroughTraffic(t *testing.T) {
	calls := map[string]simulation.Call{}
	for port := 80; port < 87; port++ {
		for _, call := range []simulation.Call{
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.Plaintext, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "foo", Alpn: "http/1.1"},
			{Port: port, Protocol: simulation.TCP, TLS: simulation.Plaintext, HostHeader: "foo"},
			{Port: port, Protocol: simulation.HTTP2, TLS: simulation.TLS, HostHeader: "foo"},
		} {
			suffix := ""
			if call.Alpn != "" {
				suffix = "-" + call.Alpn
			}
			calls[fmt.Sprintf("%v-%v-%v%v", call.Protocol, call.TLS, port, suffix)] = call
		}
	}
	ports := `
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: auto
    number: 81
  - name: tcp
    number: 82
    protocol: TCP
  - name: tls
    number: 83
    protocol: TLS
  - name: https
    number: 84
    protocol: HTTPS
  - name: grpc
    number: 85
    protocol: GRPC
  - name: h2
    number: 86
    protocol: HTTP2`

	isHTTPPort := func(p int) bool {
		switch p {
		case 80, 85, 86:
			return true
		default:
			return false
		}
	}
	isAutoPort := func(p int) bool {
		switch p {
		case 81:
			return true
		default:
			return false
		}
	}
	for _, tp := range []meshconfig.MeshConfig_OutboundTrafficPolicy_Mode{
		meshconfig.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY,
		meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY,
	} {
		t.Run(tp.String(), func(t *testing.T) {
			o := xds.FakeOptions{
				MeshConfig: func() *meshconfig.MeshConfig {
					m := mesh.DefaultMeshConfig()
					m.OutboundTrafficPolicy.Mode = tp
					return &m
				}(),
			}
			expectedCluster := map[meshconfig.MeshConfig_OutboundTrafficPolicy_Mode]string{
				meshconfig.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY: util.BlackHoleCluster,
				meshconfig.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY:     util.PassthroughCluster,
			}[tp]
			t.Run("with VIP", func(t *testing.T) {
				testCalls := []simulation.Expect{}
				for name, call := range calls {
					e := simulation.Expect{
						Name: name,
						Call: call,
						Result: simulation.Result{
							ClusterMatched: expectedCluster,
						},
					}
					// For blackhole, we will 502 where possible instead of blackhole cluster
					// This only works for HTTP on HTTP
					if expectedCluster == util.BlackHoleCluster && call.IsHTTP() && isHTTPPort(call.Port) {
						e.Result.ClusterMatched = ""
						e.Result.VirtualHostMatched = util.BlackHole
					}
					testCalls = append(testCalls, e)
				}
				sort.Slice(testCalls, func(i, j int) bool {
					return testCalls[i].Name < testCalls[j].Name
				})
				runSimulationTest(t, nil, o,
					simulationTest{
						config: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - istio.io
  addresses: [1.2.3.4]
  location: MESH_EXTERNAL
  resolution: DNS` + ports,
						calls: testCalls,
					})
			})
			t.Run("without VIP", func(t *testing.T) {
				testCalls := []simulation.Expect{}
				for name, call := range calls {
					e := simulation.Expect{
						Name: name,
						Call: call,
						Result: simulation.Result{
							ClusterMatched: expectedCluster,
						},
					}
					// For blackhole, we will 502 where possible instead of blackhole cluster
					// This only works for HTTP on HTTP
					if expectedCluster == util.BlackHoleCluster && call.IsHTTP() && (isHTTPPort(call.Port) || isAutoPort(call.Port)) {
						e.Result.ClusterMatched = ""
						e.Result.VirtualHostMatched = util.BlackHole
					}
					// TCP without a VIP will capture everything.
					// Auto without a VIP is similar, but HTTP happens to work because routing is done on header
					if call.Port == 82 || (call.Port == 81 && !call.IsHTTP()) {
						e.Result.Error = nil
						e.Result.ClusterMatched = ""
					}
					testCalls = append(testCalls, e)
				}
				sort.Slice(testCalls, func(i, j int) bool {
					return testCalls[i].Name < testCalls[j].Name
				})
				runSimulationTest(t, nil, o,
					simulationTest{
						config: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: se
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  resolution: DNS` + ports,
						calls: testCalls,
					})
			})
		})
	}
}

func TestLoop(t *testing.T) {
	runSimulationTest(t, nil, xds.FakeOptions{}, simulationTest{
		calls: []simulation.Expect{
			{
				Name: "direct request to outbound port",
				Call: simulation.Call{
					Port:     15001,
					Protocol: simulation.TCP,
				},
				Result: simulation.Result{
					// This request should be blocked
					ClusterMatched: "BlackHoleCluster",
				},
			},
			{
				Name: "direct request to inbound port",
				Call: simulation.Call{
					Port:     15006,
					Protocol: simulation.TCP,
				},
				Result: simulation.Result{
					// This request should be blocked
					ClusterMatched: "BlackHoleCluster",
				},
			},
		},
	})
}
