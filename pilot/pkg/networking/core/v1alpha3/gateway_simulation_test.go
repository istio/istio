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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/pkg/env"
)

func TestDisablePortTranslation(t *testing.T) {
	virtualServices := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "example.com"
  gateways:
  - gateway80
  - gateway8081
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
  - "example.com"
  gateways:
  - gateway80
  - gateway8081
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: b
        port:
          number: 8081`
	runGatewayTest(t,
		simulationTest{
			name: "multiple target port, target port first",
			config: createGateway("gateway80", "", `
port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + createGateway("gateway8081", "", `
port:
  number: 8081
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
  labels:
    experimental.istio.io/disable-gateway-port-translation: "true"
spec:
  hosts: ["a.example.com"]
  ports:
  - number: 80
    targetPort: 8081
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
    labels:
      istio: ingressgateway
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance-2
  namespace: istio-system
spec:
  hosts: ["b.example.com"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
    labels:
      istio: ingressgateway
---
` + virtualServices,
			calls: []simulation.Expect{
				{
					Name: "target port 1",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "example.com:80",
					},
				},
				{
					Name: "target port 2",
					Call: simulation.Call{
						Port:       8081,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8081",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8081",
						VirtualHostMatched: "example.com:8081",
					},
				},
			},
		},
		simulationTest{
			name: "multiple target port, service port first",
			config: createGateway("gateway80", "", `
port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + createGateway("gateway8081", "", `
port:
  number: 8081
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
  labels:
    experimental.istio.io/disable-gateway-port-translation: "true"
spec:
  hosts: ["b.example.com"]
  ports:
  - number: 80
    targetPort: 8081
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
    labels:
      istio: ingressgateway
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance-2
  namespace: istio-system
spec:
  hosts: ["a.example.com"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
    labels:
      istio: ingressgateway
---
` + virtualServices,
			calls: []simulation.Expect{
				{
					Name: "target port 1",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "example.com:80",
					},
				},
				{
					Name: "target port 2",
					Call: simulation.Call{
						Port:       8081,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8081",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8081",
						VirtualHostMatched: "example.com:8081",
					},
				},
			},
		})
}

func TestHTTPGateway(t *testing.T) {
	httpServer := `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "foo.bar"`
	simpleRoute := `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vs
spec:
  hosts:
  - "example.com"
  gateways:
  - gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: b
---
`
	runGatewayTest(t,
		simulationTest{
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
						Error:              simulation.ErrNoRoute,
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
		simulationTest{
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
		simulationTest{
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
		},
		simulationTest{
			name: "httpsRedirect without routes",
			config: createGateway("gateway", "", `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
tls:
  httpsRedirect: true`),
			calls: []simulation.Expect{
				{
					"request",
					simulation.Call{
						Port:       80,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						Error:              simulation.ErrTLSRedirect,
						ListenerMatched:    "0.0.0.0_80",
						VirtualHostMatched: "example.com:80",
						RouteConfigMatched: "http.80",
						StrictMatch:        true,
					},
				},
			},
		},
		simulationTest{
			// This should be the same as without, we elide the routes anyways since they always
			// redirect
			name: "httpsRedirect with routes",
			config: createGateway("gateway", "", `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
tls:
  httpsRedirect: true`) + simpleRoute,
			calls: []simulation.Expect{
				{
					"request",
					simulation.Call{
						Port:       80,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						Error:              simulation.ErrTLSRedirect,
						ListenerMatched:    "0.0.0.0_80",
						VirtualHostMatched: "example.com:80",
						RouteConfigMatched: "http.80",
						StrictMatch:        true,
					},
				},
			},
		},
		simulationTest{
			// A common example of when this would occur is when a user defines their Gateway with
			// httpsRedirect, then cert-manager creates an Ingress which requires sending HTTP
			// traffic Unfortunately, Envoy doesn't have a good way to do HTTPS redirect per-route.
			name: "mixed httpsRedirect with routes",
			config: createGateway("gateway", "", `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
tls:
  httpsRedirect: true`) + createGateway("gateway2", "", `port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"`) + simpleRoute,
			calls: []simulation.Expect{
				{
					"request",
					simulation.Call{
						Port:       80,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					simulation.Result{
						Error:              simulation.ErrTLSRedirect,
						ListenerMatched:    "0.0.0.0_80",
						VirtualHostMatched: "example.com:80",
						RouteConfigMatched: "http.80",
						StrictMatch:        true,
					},
				},
			},
		},
		simulationTest{
			name: "httpsRedirect on https",
			config: createGateway("gateway", "", `port:
  number: 443
  name: https
  protocol: HTTPS
hosts:
- "example.com"
tls:
  httpsRedirect: true
  mode: SIMPLE
  credentialName: test`) + simpleRoute,
			calls: []simulation.Expect{
				{
					"request",
					simulation.Call{
						Port:       443,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
						TLS:        simulation.TLS,
					},
					simulation.Result{
						ListenerMatched:    "0.0.0.0_443",
						VirtualHostMatched: "example.com:443",
						RouteConfigMatched: "https.443.https.gateway.default",
						ClusterMatched:     "outbound|443||b.default",
						StrictMatch:        true,
					},
				},
			},
		},
	)
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
		simulationTest{
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
		simulationTest{
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
		simulationTest{
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
					simulation.Call{Port: 443, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "foo.bar"},
					simulation.Result{Error: simulation.ErrMultipleFilterChain},
				},
			},
		},
		simulationTest{
			name: "duplicate tls virtual service",
			// Create the same virtual service in two namespaces
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: ingressgateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - hosts:
    - '*.example.com'
    port:
      name: https
      number: 443
      protocol: HTTPS
    tls:
      mode: PASSTHROUGH
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vs1
  namespace: default
spec:
  gateways:
  - istio-system/ingressgateway
  hosts:
  - mysite.example.com
  tls:
  - match:
    - port: 443
      sniHosts:
      - mysite.example.com
    route:
    - destination:
        host: mysite.default.svc.cluster.local
        port:
          number: 443
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vs2
  namespace: default
spec:
  gateways:
  - istio-system/ingressgateway
  hosts:
  - mysite.example.com
  tls:
  - match:
    - port: 443
      sniHosts:
      - mysite.example.com
    route:
    - destination:
        host: mysite.default.svc.cluster.local
        port:
          number: 443
`,
			calls: []simulation.Expect{
				{
					"call",
					simulation.Call{Port: 443, Protocol: simulation.HTTP, TLS: simulation.TLS, HostHeader: "mysite.example.com"},
					simulation.Result{
						ListenerMatched: "0.0.0.0_443",
						ClusterMatched:  "outbound|443||mysite.default.svc.cluster.local",
					},
				},
			},
		},
		simulationTest{
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
		simulationTest{
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
						Error:              simulation.ErrNoRoute,
						ListenerMatched:    "0.0.0.0_80",
						RouteConfigMatched: "http.80",
						VirtualHostMatched: "blackhole:80",
					},
				},
			},
		},
		simulationTest{
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
	runGatewayTest(t, simulationTest{
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
					Protocol:   simulation.HTTP,
					TLS:        simulation.TLS,
				},
				simulation.Result{
					Error:              simulation.ErrNoRoute,
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress-default.istio-system",
					VirtualHostMatched: "blackhole:443",
				},
			},
			{
				"https beta",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTP,
					TLS:        simulation.TLS,
				},
				simulation.Result{
					Error:              simulation.ErrNoRoute,
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress-default.istio-system",
					VirtualHostMatched: "blackhole:443",
				},
			},
		},
	}, simulationTest{
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
					Protocol:   simulation.HTTP,
					TLS:        simulation.TLS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress-default.istio-system",
					ClusterMatched:     "outbound|80||alpha.default.svc.cluster.local",
				},
			},
			{
				"https beta",
				simulation.Call{
					Port:       443,
					HostHeader: "example.com",
					Path:       "/beta",
					Protocol:   simulation.HTTP,
					TLS:        simulation.TLS,
				},
				simulation.Result{
					ListenerMatched:    "0.0.0.0_443",
					RouteConfigMatched: "https.443.https-443-ingress-alpha-default-0.alpha-istio-autogenerated-k8s-ingress-default.istio-system",
					ClusterMatched:     "outbound|80||beta.default.svc.cluster.local",
				},
			},
		},
	})
}

type simulationTest struct {
	name       string
	config     string
	kubeConfig string
	// skipValidation disables validation of XDS resources. Should be used only when we expect a failure (regression catching)
	skipValidation bool
	calls          []simulation.Expect
}

var debugMode = env.RegisterBoolVar("SIMULATION_DEBUG", true, "if enabled, will dump verbose output").Get()

func runGatewayTest(t *testing.T, cases ...simulationTest) {
	for _, tt := range cases {
		proxy := &model.Proxy{
			Metadata: &model.NodeMetadata{
				Labels:    map[string]string{"istio": "ingressgateway"},
				Namespace: "istio-system",
			},
			Type: model.Router,
		}
		runSimulationTest(t, proxy, xds.FakeOptions{}, tt)
	}
}

func runSimulationTest(t *testing.T, proxy *model.Proxy, o xds.FakeOptions, tt simulationTest) {
	runTest := func(t *testing.T) {
		o.ConfigString = tt.config
		o.KubernetesObjectString = tt.kubeConfig
		s := xds.NewFakeDiscoveryServer(t, o)
		sim := simulation.NewSimulation(t, s, s.SetupProxy(proxy))
		sim.RunExpectations(tt.calls)
		if t.Failed() && debugMode {
			t.Log(xdstest.MapKeys(xdstest.ExtractClusters(sim.Clusters)))
			t.Log(xdstest.ExtractListenerNames(sim.Listeners))
			t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Listeners)))
			t.Log(xdstest.DumpList(t, xdstest.InterfaceSlice(sim.Routes)))
			t.Log(tt.config)
			t.Log(tt.kubeConfig)
		}
		t.Run("validate configs", func(t *testing.T) {
			if tt.skipValidation {
				t.Skip()
			}
			xdstest.ValidateClusters(t, sim.Clusters)
			xdstest.ValidateListeners(t, sim.Listeners)
			xdstest.ValidateRouteConfigurations(t, sim.Routes)
		})
	}
	if tt.name != "" {
		t.Run(tt.name, runTest)
	} else {
		runTest(t)
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

func createGatewayWithServiceSelector(name, service string, servers ...string) string {
	if name == "" {
		name = "default"
	}
	return tmpl.MustEvaluate(`apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: "{{.Name}}"
  namespace: "istio-system"
  annotations:
    internal.istio.io/gateway-service: "{{.Service}}"
spec:
  servers:
{{- range $i, $p := $.Servers }}
  -
{{$p | trim | indent 4}}
{{- end }}
---
`, struct {
		Name    string
		Service string
		Servers []string
	}{name, service, servers})
}

func TestTargetPort(t *testing.T) {
	runGatewayTest(t,
		simulationTest{
			name: "basic",
			config: createGatewayWithServiceSelector("gateway80", "istio-ingressgateway.istio-system.svc.cluster.local", `
port:
 number: 80
 name: http
 protocol: HTTP
hosts:
- "example.com"
`) + createGatewayWithServiceSelector("gateway81", "istio-ingressgateway.istio-system.svc.cluster.local", `
port:
 number: 8080
 name: http
 protocol: HTTP
hosts:
- "example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
 name: service-instance
 namespace: istio-system
spec:
 hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
 ports:
 - number: 80
   targetPort: 8080
   name: http
   protocol: HTTP
 resolution: STATIC
 location: MESH_INTERNAL
 endpoints:
 - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
 name: a
spec:
 hosts:
 - "example.com"
 gateways:
 - istio-system/gateway80
 - istio-system/gateway81
 http:
 - match:
   - uri:
       prefix: /
   route:
   - destination:
       host: a
       port:
         number: 80`,
			calls: []simulation.Expect{
				{
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "example.com:80",
					},
				},
			},
		},
		simulationTest{
			name: "multiple target port",
			config: createGatewayWithServiceSelector("gateway80", "ingress1.istio-system.svc.cluster.local", `
port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + createGatewayWithServiceSelector("gateway81", "ingress2.istio-system.svc.cluster.local", `
port:
  number: 80
  name: http
  protocol: HTTP
hosts:
- "example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["ingress1.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance-2
  namespace: istio-system
spec:
  hosts: ["ingress2.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8081
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "example.com"
  gateways:
  - istio-system/gateway80
  - istio-system/gateway81
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
  - "example.com"
  gateways:
  - istio-system/gateway80
  - istio-system/gateway81
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: b
        port:
          number: 8081`,
			calls: []simulation.Expect{
				{
					Name: "target port 1",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "example.com:80",
					},
				},
				{
					Name: "target port 2",
					Call: simulation.Call{
						Port:       8081,
						HostHeader: "example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8081",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8081",
						VirtualHostMatched: "example.com:80",
					},
				},
			},
		})
}

func TestGatewayServices(t *testing.T) {
	runGatewayTest(t,
		simulationTest{
			name: "single service",
			config: createGatewayWithServiceSelector("gateway", "istio-ingressgateway.istio-system.svc.cluster.local", `
port:
  number: 80
  name: http-80
  protocol: HTTP
hosts:
- "80.example.com"
`, `
port:
  number: 82
  name: http-82
  protocol: HTTP
hosts:
- "82.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "*.example.com"
  gateways:
  - istio-system/gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: a
        port:
          number: 80`,
			calls: []simulation.Expect{
				{
					Name: "port 8080",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "80.example.com:80",
					},
				},
				{
					Name: "no service match",
					Call: simulation.Call{
						Port:       82,
						HostHeader: "81.example.com",
						Protocol:   simulation.HTTP,
					},
					// For gateway service reference, if there is no matching port we do not setup a listener
					Result: simulation.Result{
						Error: simulation.ErrNoListener,
					},
				},
			},
		},
		simulationTest{
			name: "wrong namespace",
			config: createGatewayWithServiceSelector("gateway", "istio-ingressgateway.not-istio-system.svc.cluster.local", `
port:
  number: 80
  name: http-80
  protocol: HTTP
hosts:
- "80.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: not-istio-system
spec:
  hosts: ["istio-ingressgateway.not-istio-system.svc.cluster.local"]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---`,
			calls: []simulation.Expect{
				{
					Name: "port 80",
					Call: simulation.Call{
						Port:       80,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						// Service selected is in another namespace, we cannot cross namespace boundary
						Error: simulation.ErrNoListener,
					},
				},
			},
		},
		simulationTest{
			name: "multiple services",
			config: createGatewayWithServiceSelector("gateway", "istio-ingressgateway.istio-system.svc.cluster.local,ingress.com", `
port:
  number: 80
  name: http-80
  protocol: HTTP
hosts:
- "80.example.com"
`, `
port:
  number: 81
  name: http-81
  protocol: HTTP
hosts:
- "81.example.com"
`, `
port:
  number: 82
  name: http-82
  protocol: HTTP
hosts:
- "82.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance2
  namespace: istio-system
spec:
  hosts: ["ingress.com"]
  ports:
  - number: 81
    targetPort: 8081
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "*.example.com"
  gateways:
  - istio-system/gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: a
        port:
          number: 80`,
			calls: []simulation.Expect{
				{
					Name: "port 8080",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "80.example.com:80",
					},
				},
				{
					Name: "port 8081",
					Call: simulation.Call{
						Port:       8081,
						HostHeader: "81.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8081",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8081",
						VirtualHostMatched: "81.example.com:81",
					},
				},
				{
					// For gateway service reference, if there is no matching port we do not setup a listener
					Name: "no service match",
					Call: simulation.Call{
						Port:       82,
						HostHeader: "81.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						Error: simulation.ErrNoListener,
					},
				},
			},
		},
		simulationTest{
			name: "multiple overlapping services",
			config: createGatewayWithServiceSelector("gateway", "istio-ingressgateway.istio-system.svc.cluster.local,ingress.com", `
port:
 number: 80
 name: http-80
 protocol: HTTP
hosts:
- "80.example.com"
`, `
port:
 number: 81
 name: http-81
 protocol: HTTP
hosts:
- "81.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
 name: service-instance
 namespace: istio-system
spec:
 hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
 ports:
 - number: 80
   targetPort: 8080
   name: http
   protocol: HTTP
 resolution: STATIC
 location: MESH_INTERNAL
 endpoints:
 - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
 name: service-instance2
 namespace: istio-system
spec:
 hosts: ["ingress.com"]
 ports:
 - number: 81
   targetPort: 8080
   name: http
   protocol: HTTP
 resolution: STATIC
 location: MESH_INTERNAL
 endpoints:
 - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
 name: a-80
spec:
 hosts:
 - "80.example.com"
 gateways:
 - istio-system/gateway
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
 name: a-81
spec:
 hosts:
 - "81.example.com"
 gateways:
 - istio-system/gateway
 http:
 - match:
   - uri:
       prefix: /
   route:
   - destination:
       host: a
       port:
         number: 80`,
			calls: []simulation.Expect{
				{
					Name: "port 8080 from 80",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "80.example.com:80",
					},
				},
				{
					Name: "port 8080 from 81",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "81.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "81.example.com:81",
					},
				},
			},
		},
		simulationTest{
			name: "multiple overlapping services wildcard",
			config: createGatewayWithServiceSelector("gateway", "istio-ingressgateway.istio-system.svc.cluster.local,ingress.com", `
port:
  number: 80
  name: http-80
  protocol: HTTP
hosts:
- "80.example.com"
`, `
port:
  number: 81
  name: http-81
  protocol: HTTP
hosts:
- "81.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance2
  namespace: istio-system
spec:
  hosts: ["ingress.com"]
  ports:
  - number: 81
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
 name: a-80
spec:
 hosts:
 - "80.example.com"
 gateways:
 - istio-system/gateway
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
 name: a-81
spec:
 hosts:
 - "81.example.com"
 gateways:
 - istio-system/gateway
 http:
 - match:
   - uri:
       prefix: /
   route:
   - destination:
       host: a
       port:
         number: 80`,
			calls: []simulation.Expect{
				{
					Name: "port 8080 from 80",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "80.example.com:80",
					},
				},
				{
					Name: "port 8080 from 81",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "81.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "81.example.com:81",
					},
				},
			},
		},
		simulationTest{
			name: "no match selector",
			config: createGateway("gateway", "istio-system", `
port:
  number: 80
  name: http-80
  protocol: HTTP
hosts:
- "80.example.com"
`, `
port:
  number: 82
  name: http-82
  protocol: HTTP
hosts:
- "82.example.com"
`) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
  ports:
  - number: 80
    targetPort: 8080
    name: http
    protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: a
spec:
  hosts:
  - "*.example.com"
  gateways:
  - istio-system/gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: a
        port:
          number: 80`,
			calls: []simulation.Expect{
				{
					Name: "port 8080",
					Call: simulation.Call{
						Port:       8080,
						HostHeader: "80.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_8080",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.8080",
						VirtualHostMatched: "80.example.com:80",
					},
				},
				{
					// For gateway selector more, if there is no matching service we use the port as is
					Name: "no service match",
					Call: simulation.Call{
						Port:       82,
						HostHeader: "82.example.com",
						Protocol:   simulation.HTTP,
					},
					Result: simulation.Result{
						ListenerMatched:    "0.0.0.0_82",
						ClusterMatched:     "outbound|80||a.default",
						RouteConfigMatched: "http.82",
						VirtualHostMatched: "82.example.com:82",
					},
				},
			},
		},
		simulationTest{
			name:           "overlapping SNI match",
			skipValidation: true, // TODO: https://github.com/istio/istio/issues/39921
			config: `apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: gw
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - hosts:
    - example.com
    port:
      name: example
      number: 443
      protocol: TLS
    tls:
      mode: PASSTHROUGH
  - hosts:
    - '*'
    port:
      name: wildcard-tls
      number: 443
      protocol: HTTPS
    tls:
      mode: PASSTHROUGH
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: example
  namespace: istio-system
spec:
  gateways:
  - gw
  hosts:
  - example.com
  tls:
  - match:
    - sniHosts:
      - example.com
    route:
    - destination:
        host: example
` + `
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-instance
  namespace: istio-system
spec:
  hosts: ["istio-ingressgateway.istio-system.svc.cluster.local"]
  ports:
  - number: 443
    targetPort: 443
    name: https
    protocol: HTTPS
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: 1.1.1.1`,
			calls: []simulation.Expect{},
		},
	)
}
