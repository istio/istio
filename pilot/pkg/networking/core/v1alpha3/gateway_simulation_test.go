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
	"testing"

	pilot_model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/simulation"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestGateway(t *testing.T) {
	cases := []struct {
		name   string
		config string
		calls  []simulation.Expect
	}{
		{
			name: "no virtual services",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: bookinfo-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "foo.bar"
`,
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
		{
			name: "simple http and virtual service",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: bookinfo-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "foo.bar"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: bookinfo
spec:
  hosts:
  - "*"
  gateways:
  - bookinfo-gateway
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
		{
			name:   "duplicate cross namespace gateway collision",
			config: fmt.Sprintf(gatewayCollision, "alpha"), // namespace comes before istio-system
			calls: []simulation.Expect{
				{
					simulation.Call{Port: 34000, Protocol: simulation.TCP},
					// TODO(https://github.com/istio/istio/issues/21394) This is a bug!
					// Should have identical result to the test below
					simulation.Result{Error: simulation.ErrNoListener},
				},
			},
		},
		{
			name:   "duplicate cross namespace gateway collision - selected first",
			config: fmt.Sprintf(gatewayCollision, "zeta"), // namespace comes after istio-system
			calls: []simulation.Expect{
				{
					simulation.Call{Port: 34000, Protocol: simulation.TCP},
					simulation.Result{ListenerMatched: "0.0.0.0_34000", ClusterMatched: "outbound|9080||productpage.default"},
				},
			},
		},
		{
			name: "duplicate tls gateway",
			// Create the same gateway in two namespaces
			config: tmpl.EvaluateOrFail(t, tlsGateway, struct{ Namespace string }{"istio-system"}) +
				tmpl.EvaluateOrFail(t, tlsGateway, struct{ Namespace string }{"default"}),
			calls: []simulation.Expect{
				{
					// TODO(https://github.com/istio/istio/issues/24638) This is a bug!
					// We should not have multiple matches, envoy will NACK this
					simulation.Call{Port: 443, Protocol: simulation.HTTPS, HostHeader: "foo.bar"},
					simulation.Result{Error: simulation.ErrMultipleFilterChain},
				},
			},
		},
		{
			name: "multiple protocols on a port",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: bookinfo-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "foo.bar"
  - port:
      number: 80
      name: tcp
      protocol: TCP
    hosts:
    - "foo.bar"
`,
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
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &pilot_model.Proxy{
				Metadata: &pilot_model.NodeMetadata{Labels: map[string]string{"istio": "ingressgateway"}},
				Type:     pilot_model.Router,
			}
			s := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
				ConfigString: tt.config,
			})
			simulation.NewSimulation(t, s, s.SetupProxy(proxy)).RunExpectations(tt.calls)
		})
	}
}

var (
	tlsGateway = `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: istio-ingressgateway
  namespace: {{.Namespace}}
spec:
  selector:
    istio: ingressgateway
  servers:
    - hosts:
        - ./*
      port:
        name: https-ingress
        number: 443
        protocol: HTTPS
      tls:
        credentialName: sds-credential
        mode: SIMPLE
---
`
	gatewayCollision = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 34000
      name: tcp
      protocol: TCP
    hosts:
    - "foo.bar"
---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
  namespace: %s # namespace matters - must be alphabetically lower than istio-system to trigger bug
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 34000
      name: tcp
      protocol: TCP
    hosts:
    - "foo.bar"
---
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
)
