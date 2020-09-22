package v1alpha3_test

import (
	"testing"

	pilot_model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/simulation"
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
