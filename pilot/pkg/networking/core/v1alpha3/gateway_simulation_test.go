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

	pilot_model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/pkg/env"
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
					// Expect listener, but no routing
					"defined port",
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
					// There will be no listener
					"undefined port",
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
					"uri mismatch",
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
					"host mismatch",
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
					"match",
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
					"a",
					simulation.Call{
						Port:       80,
						HostHeader: "a.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{ClusterMatched: "outbound|80||a.default"},
				},
				{
					"b",
					simulation.Call{
						Port:       80,
						HostHeader: "b.example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{ClusterMatched: "outbound|80||b.default"},
				},
				{
					"undefined hostname",
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
					"call",
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
					"call",
					simulation.Call{Port: 80, Protocol: simulation.TCP},
					simulation.Result{ListenerMatched: "0.0.0.0_80", ClusterMatched: "outbound|9080||productpage.default"},
				},
			},
		},
		gatewayTest{
			name:           "duplicate tls gateway",
			skipValidation: true,
			// Create the same gateway in two namespaces
			config: createGateway("", "istio-system", tlsServer) +
				createGateway("", "default", tlsServer),
			calls: []simulation.Expect{
				{
					// TODO(https://github.com/istio/istio/issues/24638) This is a bug!
					// We should not have multiple matches, envoy will NACK this
					"call",
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
					"call tcp",
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
					"call http",
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
					"ns-1",
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
					"ns-2",
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

func TestIngress(t *testing.T) {
	cfg := `
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: {{.Name}}
  namespace: default
  creationTimestamp: "{{.Time}}"
  annotations:
    kubernetes.io/ingress.class: istio
spec:
  rules:
  - host: example.com
    http:
      paths:
      - backend:
          serviceName: {{.Name}}
          servicePort: 80
        path: /{{.Name}}
  tls:
  - hosts:
    - example.com
    secretName: ingressgateway-certs
---`
	runGatewayTest(t, gatewayTest{
		name: "ingress shared TLS cert conflict - beta first",
		// TODO(https://github.com/istio/istio/issues/24385) this is a bug
		// "alpha" is created after "beta". This triggers a mismatch in the conflict resolution logic in Ingress and VirtualService, leading to unexpected results
		kubeConfig: tmpl.MustEvaluate(cfg, map[string]string{"Name": "alpha", "Time": "2020-01-01T00:00:00Z"}) +
			tmpl.MustEvaluate(cfg, map[string]string{"Name": "beta", "Time": "2010-01-01T00:00:00Z"}),
		calls: []simulation.Expect{
			{
				"http alpha",
				simulation.Call{
					Port:       80,
					HostHeader: "example.com",
					Path:       "/alpha",
					Protocol:   simulation.HTTP,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_80",
					RouteConfigMatched: "http.80",
					ClusterMatched:     "outbound|80||alpha.default.svc.cluster.local",
				},
			},
			{
				"http beta",
				simulation.Call{
					Port:       80,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTP,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_80",
					RouteConfigMatched: "http.80",
					ClusterMatched:     "outbound|80||beta.default.svc.cluster.local",
				},
			},
			{
				"https alpha",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/alpha",
					Protocol:   simulation.HTTPS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress.istio-system",
					VirtualHostMatched: "blackhole:443",
				},
			},
			{
				"https beta",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTPS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress.istio-system",
					VirtualHostMatched: "blackhole:443",
				},
			},
		},
	}, gatewayTest{
		name: "ingress shared TLS cert conflict - alpha first",
		// "alpha" is created before "beta". This avoids the bug in the previous test
		kubeConfig: tmpl.MustEvaluate(cfg, map[string]string{"Name": "alpha", "Time": "2010-01-01T00:00:00Z"}) +
			tmpl.MustEvaluate(cfg, map[string]string{"Name": "beta", "Time": "2020-01-01T00:00:00Z"}),
		calls: []simulation.Expect{
			{
				"http alpha",
				simulation.Call{
					Port:       80,
					HostHeader: "example.com",
					Path:       "/alpha",
					Protocol:   simulation.HTTP,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_80",
					RouteConfigMatched: "http.80",
					ClusterMatched:     "outbound|80||alpha.default.svc.cluster.local",
				},
			},
			{
				"http beta",
				simulation.Call{
					Port:       80,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTP,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_80",
					RouteConfigMatched: "http.80",
					ClusterMatched:     "outbound|80||beta.default.svc.cluster.local",
				},
			},
			{
				"https alpha",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/alpha",
					Protocol:   simulation.HTTPS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress.istio-system",
					ClusterMatched:     "outbound|80||alpha.default.svc.cluster.local",
				},
			},
			{
				"https beta",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTPS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress.istio-system",
					ClusterMatched:     "outbound|80||beta.default.svc.cluster.local",
				},
			},
		},
	})
}

type gatewayTest struct {
	name       string
	config     string
	kubeConfig string
	// skipValidation disables validation of XDS resources. Should be used only when we expect a failure (regression catching)
	skipValidation bool
	calls          []simulation.Expect
}

var debugMode = env.RegisterBoolVar("SIMULATION_DEBUG", false, "if enabled, will dump verbose output").Get()

func runGatewayTest(t *testing.T, cases ...gatewayTest) {
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{Labels: map[string]string{"istio": "ingressgateway"}},
				Type:     pilot_model.Router,
			}
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				ConfigString:           tt.config,
				KubernetesObjectString: tt.kubeConfig,
			})
			sim := simulation.NewSimulation(t, s, s.SetupProxy(proxy))
			sim.RunExpectations(tt.calls)
			if t.Failed() && debugMode {
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Clusters)))
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Listeners)))
				t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Routes)))
				t.Log(tt.config)
			}
			if !tt.skipValidation {
				xdstest.ValidateClusters(t, sim.Clusters)
				xdstest.ValidateListeners(t, sim.Listeners)
				xdstest.ValidateRouteConfigurations(t, sim.Routes)
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
