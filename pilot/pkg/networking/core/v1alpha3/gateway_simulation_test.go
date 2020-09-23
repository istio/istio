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
	"testing"

	"istio.io/pkg/env"

	pilot_model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestHTTPGateway(t *testing.T) {
	httpServer := `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "foo.bar"`
	runGatewayTest(t,
		gatewayTest{
			name:   "no virtual services",
			config: createGateway("", "", httpServer),
			calls: []simulation.Expect{
				{
					// Port define in gateway, but no virtual services
					// Expect a 404
					simulation.Call{
						Port:       80,
						HostHeader: "foo.bar",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						VirtualHostMatched: "blackhole:80",
					},
				},
				{
					// Port not defined at all. There should be no listener.
					simulation.Call{
						Port:       81,
						HostHeader: "foo.bar",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						Error: simulation.ErrNoListener,
					},
				},
			},
		},
		gatewayTest{
			name: "simple http and virtual service",
			config: createGateway("gateway", "", httpServer) + `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: bookinfo
spec:
  hosts:
  - "*"
  gateways:
  - gateway
  http:
  - match:
    - uri:
        exact: /productpage
    route:
    - destination:
        host: productpage
        port:
          number: 9080
`,
			calls: []simulation.Expect{
				{
					// Do not match any URI
					simulation.Call{
						Port:       80,
						HostHeader: "foo.bar",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						// We didn't match the URI
						Error:              simulation.ErrNoRoute,
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						VirtualHostMatched: "foo.bar:80",
					},
				},
				{
					// Do not match host
					simulation.Call{
						Port:       80,
						HostHeader: "bad.bar",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						// We didn't match the host
						Error:              simulation.ErrNoVirtualHost,
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
					},
				},
				{
					// Match everything
					simulation.Call{
						Port:       80,
						HostHeader: "foo.bar",
						Path:       "/productpage",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_80",
						VirtualHostMatched: "foo.bar:80",
						ClusterMatched:     "outbound|9080||productpage.default",
					},
				},
			},
		},
		gatewayTest{
			name: "virtual service merging",
			config: createGateway("gateway", "", `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "*.example.com"`) + `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "a.example.com"
  gateways:
  - gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: a
        port:
          number: 80
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: b
spec:
  hosts:
  - "b.example.com"
  gateways:
  - gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: b
        port:
          number: 80
`,
			calls: []simulation.Expect{
				{
					simulation.Call{
						Port:       80,
						HostHeader: "a.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{ClusterMatched: "outbound|80||a.default"},
				},
				{
					simulation.Call{
						Port:       80,
						HostHeader: "b.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{ClusterMatched: "outbound|80||b.default"},
				},
				{
					simulation.Call{
						Port:       80,
						HostHeader: "c.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{Error: simulation.ErrNoVirtualHost},
				},
			},
		})
}

func TestGatewayConflicts(t *testing.T) {
	tcpServer := `port:
  number: 80
  name: tcp
  protocol: TCP
hosts:
- "foo.bar"`
	httpServer := `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "foo.bar"`
	tlsServer := `hosts:
  - ./*
port:
  name: https-ingress
  number: 443
  protocol: HTTPS
tls:
  credentialName: sds-credential
  mode: SIMPLE`
	gatewayCollision := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: bookinfo
spec:
  hosts:
  - "*"
  gateways:
  - istio-system/gateway
  tcp:
  - route:
    - destination:
        host: productpage
        port:
          number: 9080
`
	runGatewayTest(t,
		gatewayTest{
			name: "duplicate cross namespace gateway collision",
			config: createGateway("gateway", "istio-system", tcpServer) +
				createGateway("gateway", "alpha", tcpServer) + // namespace comes before istio-system

				gatewayCollision,
			calls: []simulation.Expect{
				{
					simulation.Call{Port: 80, Protocol: simulation.TCP},
					// TODO(https://github.com/istio/istio/issues/21394) This is a bug!
					// Should have identical result to the test below
					simulation.Result{Error: simulation.ErrNoListener},
				},
			},
		},
		gatewayTest{
			name: "duplicate cross namespace gateway collision - selected first",
			config: createGateway("gateway", "istio-system", tcpServer) +
				createGateway("gateway", "zeta", tcpServer) + // namespace comes after istio-system
				gatewayCollision,
			calls: []simulation.Expect{
				{
					simulation.Call{Port: 80, Protocol: simulation.TCP},
					simulation.Result{ListenerMatched: "0.0.0.0_80", ClusterMatched: "outbound|9080||productpage.default"},
				},
			},
		},
		gatewayTest{
			name: "duplicate tls gateway",
			// Create the same gateway in two namespaces
			config: createGateway("", "istio-system", tlsServer) +
				createGateway("", "default", tlsServer),
			calls: []simulation.Expect{
				{
					// TODO(https://github.com/istio/istio/issues/24638) This is a bug!
					// We should not have multiple matches, envoy will NACK this
					simulation.Call{Port: 443, Protocol: simulation.HTTPS, HostHeader: "foo.bar"},
					simulation.Result{Error: simulation.ErrMultipleFilterChain},
				},
			},
		},
		gatewayTest{
			// TODO(https://github.com/istio/istio/issues/27481) this may be a bug. At very least, this should have indication to user
			name: "multiple protocols on a port - tcp first",
			config: createGateway("alpha", "", tcpServer) +
				createGateway("beta", "", httpServer),
			calls: []simulation.Expect{
				{
					// TCP takes precedence. Since we have no tcp routes, this will result in no listeners
					simulation.Call{
						Port:     80,
						Protocol: simulation.TCP,
					},
					simulation.Result{
						Error: simulation.ErrNoListener,
					},
				},
			},
		},
		gatewayTest{
			// TODO(https://github.com/istio/istio/issues/27481) this may be a bug. At very least, this should have indication to user
			name: "multiple protocols on a port - http first",
			config: createGateway("beta", "", tcpServer) +
				createGateway("alpha", "", httpServer),
			calls: []simulation.Expect{
				{
					// Port define in gateway, but no virtual services
					// Expect a 404
					// HTTP protocol takes precedence
					simulation.Call{
						Port:       80,
						HostHeader: "foo.bar",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						VirtualHostMatched: "blackhole:80",
					},
				},
			},
		},
		gatewayTest{
			name: "multiple wildcards with virtual service disambiguator",
			config: createGateway("alpha", "", `
hosts:
  - ns-1/*.example.com
port:
  name: http
  number: 80
  protocol: HTTP`) +
				createGateway("beta", "", `
hosts:
  - ns-2/*.example.com
port:
  name: http
  number: 80
  protocol: HTTP`) + `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
  namespace: ns-1
spec:
  hosts:
  - "ns-1.example.com"
  gateways:
  - default/alpha
  http:
  - route:
    - destination:
        host: echo
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
  namespace: ns-2
spec:
  hosts:
  - "ns-2.example.com"
  gateways:
  - default/beta
  http:
  - route:
    - destination:
        host: echo
`,
			calls: []simulation.Expect{
				{
					simulation.Call{
						Port:       80,
						HostHeader: "ns-1.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						ClusterMatched:     "outbound|80||echo.ns-1",
					},
				},
				{
					simulation.Call{
						Port:       80,
						HostHeader: "ns-2.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						ClusterMatched:     "outbound|80||echo.ns-2",
					},
				},
			},
		},
	)
}

type gatewayTest struct {
	name   string
	config string
	calls  []simulation.Expect
}

var debugMode = env.RegisterBoolVar("SIMULATION_DEBUG", true, "if enabled, will dump verbose output").Get()

func runGatewayTest(t *testing.T, cases ...gatewayTest) {
	for _, tt := range cases {
		if tt.name != "multiple wildcards with virtual service disambiguator" {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			proxy := &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{Labels: map[string]string{"istio": "ingressgateway"}},
				Type:     pilot_model.Router,
			}
			s := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
				ConfigString: tt.config,
			})
			sim := simulation.NewSimulation(t, s, s.SetupProxy(proxy))
			sim.RunExpectations(tt.calls)
			if t.Failed() && debugMode {
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Clusters)))
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Listeners)))
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Routes)))
				t.Log(tt.config)
			}
		})
	}
}

func createGateway(name, namespace string, servers ...string) string {
	if name == "" {
		name = "default"
	}
	if namespace == "" {
		namespace = "default"
	}
	return tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: "{{.Name}}"
  namespace: "{{.Namespace}}"
spec:
  selector:
    istio: ingressgateway
  servers:
{{- range $i, $p := $.Servers }}
  -
{{$p | trim | indent 4}}
{{- end }}
---
`, struct {
		Name      string
		Namespace string
		Servers   []string
	}{name, namespace, servers})
}
