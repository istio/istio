// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package core_test

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/tmpl"
)

const se = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: se1
spec:
  hosts:
  - blah1.somedomain
  addresses:
  - {{index .Addresses 0}}
  ports:
  - number: 9999
    name: port-9999
    protocol: {{.Protocol}}
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: se2
spec:
  hosts:
  - blah2.somedomain
  addresses:
  - {{index .Addresses 1}}
  ports:
  - number: 9999
    name: port-9999
    protocol: {{.Protocol}}
---
{{ if .VirtualService }}
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: vs1
spec:
  hosts:
  - blah1.somedomain
  tls:
  - match:
    - port: 9999
      sniHosts:
      - blah1.somedomain
    route:
    - destination:
        host: blah1.somedomain
        port:
          number: 9999
{{ end }}`

func TestServiceEntry(t *testing.T) {
	type Input struct {
		Addresses      []string
		Protocol       string
		VirtualService bool
	}
	cases := []simulationTest{
		{
			name: "identical CIDR (ignoring insignificant bits) is dropped",
			config: tmpl.MustEvaluate(se, Input{
				Addresses: []string{"1234:1f1:123:123:f816:3eff:feb8:2287/32", "1234:1f1:123:123:f816:3eff:febf:57ce/32"},
				Protocol:  "TCP",
			}),
			kubeConfig: "",
			calls: []simulation.Expect{{
				// Expect listener, but no routing
				Name: "defined port",
				Call: simulation.Call{
					Port:     9999,
					Address:  "1234:1f1:1:1:1:1:1:1",
					Protocol: simulation.TCP,
				},
				Result: simulation.Result{
					ListenerMatched: "0.0.0.0_9999",
					ClusterMatched:  "outbound|9999||blah1.somedomain",
				},
			}},
		},
		{
			name: "overlapping CIDR",
			config: tmpl.MustEvaluate(se, Input{
				Addresses: []string{"1234:1f1:123:123:f816:3eff:feb8:2287/16", "1234:1f1:123:123:f816:3eff:febf:57ce/32"},
				Protocol:  "TCP",
			}),
			kubeConfig: "",
			calls: []simulation.Expect{
				{
					Name: "match the more precise one",
					Call: simulation.Call{
						Port: 9999,
						// Matches both, but more closely matches blah2
						Address: "1234:1f1:123:123:f816:3eff:febf:57ce",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "outbound|9999||blah2.somedomain",
					},
				},
				{
					Name: "match the more less one",
					Call: simulation.Call{
						Port: 9999,
						// Only matches the first
						Address: "1234::1",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "outbound|9999||blah1.somedomain",
					},
				},
			},
		},
		{
			name: "overlapping CIDR with SNI",
			config: tmpl.MustEvaluate(se, Input{
				Addresses:      []string{"1234:1f1:123:123:f816:3eff:feb8:2287/16", "1234:1f1:123:123:f816:3eff:febf:57ce/32"},
				Protocol:       "TLS",
				VirtualService: true,
			}),
			kubeConfig: "",
			calls: []simulation.Expect{
				{
					Name: "IP and SNI match: longer IP",
					Call: simulation.Call{
						Port: 9999,
						TLS:  simulation.TLS,
						Sni:  "blah2.somedomain",
						// Matches both, but more closely matches blah2, but SNI is blah1
						Address: "1234:1f1:123:123:f816:3eff:febf:57ce",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "outbound|9999||blah2.somedomain",
					},
				},
				{
					Name: "IP and SNI match: shorter IP",
					Call: simulation.Call{
						Port: 9999,
						Sni:  "blah1.somedomain",
						// Only matches the first
						Address: "1234::1",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "outbound|9999||blah1.somedomain",
					},
				},
				{
					Name: "Mismatch IP and SNI match: longer IP",
					Call: simulation.Call{
						Port: 9999,
						TLS:  simulation.TLS,
						Sni:  "blah1.somedomain",
						// Matches both, but more closely matches blah2, but SNI is blah1
						Address: "1234:1f1:123:123:f816:3eff:febf:57ce",
					},
					// TODO(https://github.com/istio/istio/issues/52235) this is broken
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "PassthroughCluster",
					},
				},
				{
					// Expect listener, but no routing
					Name: "Mismatch IP and SNI match: shorter IP",
					Call: simulation.Call{
						Port: 9999,
						Sni:  "blah2.somedomain",
						// Only matches the first
						Address: "1234::1",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "0.0.0.0_9999",
						ClusterMatched:  "PassthroughCluster",
					},
				},
			},
		},
		{
			// Regression test for https://github.com/istio/istio/issues/52847
			name: "partial overlap of IP",
			config: tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: se1
spec:
  hosts:
  - blah1.somedomain
  addresses: {{ . |toJson }}
  ports:
  - number: 9999
    name: port-9999
    protocol: TCP`, []string{"1.1.1.1"}) + "\n---\n" + tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: se2
spec:
  hosts:
  - blah2.somedomain
  addresses: {{ . |toJson }}
  ports:
  - number: 9999
    name: port-9999
    protocol: TCP`, []string{"2.2.2.2", "1.1.1.1"}),
			kubeConfig: "",
			calls: []simulation.Expect{
				{
					Name: "unique IP",
					Call: simulation.Call{
						Port: 9999,
						// Matches both, but more closely matches blah2, but SNI is blah1
						Address: "2.2.2.2",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "2.2.2.2_9999",
						ClusterMatched:  "outbound|9999||blah2.somedomain",
					},
				},
				{
					Name: "shared IP",
					Call: simulation.Call{
						Port: 9999,
						// Matches both, but more closely matches blah2, but SNI is blah1
						Address: "1.1.1.1",
					},
					Result: simulation.Result{
						Error:           nil,
						ListenerMatched: "1.1.1.1_9999",
						ClusterMatched:  "outbound|9999||blah1.somedomain",
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Labels:      map[string]string{"app": "foo"},
				Metadata:    &model.NodeMetadata{Labels: map[string]string{"app": "foo"}, ClusterID: "Kubernetes"},
				IPAddresses: []string{"127.0.0.1", "1234::1234"},
			}
			proxy.DiscoverIPMode()
			runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
				name:   tt.name,
				config: tt.config,
				calls:  tt.calls,
			})
		})
	}
}

const serviceEntriesWithDuplicatedHosts = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: istio-http
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: DNS
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: istio-https
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: DNS
`

func TestServiceEntryDuplicatedHostname(t *testing.T) {
	// Only the old IP auto-allocator has this functionality
	// TODO(https://github.com/istio/istio/issues/53676): implement this in the new one, and test both
	test.SetForTest(t, &features.EnableIPAutoallocate, false)
	cases := []simulationTest{
		{
			name:   "service entries with reused hosts should have auto allocated the same IP address",
			config: serviceEntriesWithDuplicatedHosts,
			calls: []simulation.Expect{
				{
					Name: "HTTP call",
					Call: simulation.Call{
						Address:    "240.240.91.120",
						Port:       80,
						HostHeader: "istio.io",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched: "0.0.0.0_80",
						ClusterMatched:  "outbound|80||istio.io",
					},
				},
				{
					Name: "HTTPS call",
					Call: simulation.Call{
						Address:    "240.240.91.120",
						Port:       443,
						HostHeader: "istio.io",
						Protocol:   simulation.HTTP,
						TLS:        simulation.TLS,
					},
					Result: simulation.Result{
						ListenerMatched: "240.240.91.120_443",
						ClusterMatched:  "outbound|443||istio.io",
					},
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Metadata: &model.NodeMetadata{
					DNSCapture:      true,
					DNSAutoAllocate: true,
				},
			}
			runSimulationTest(t, proxy, xds.FakeOptions{}, simulationTest{
				name:   tt.name,
				config: tt.config,
				calls:  tt.calls,
			})
		})
	}
}
