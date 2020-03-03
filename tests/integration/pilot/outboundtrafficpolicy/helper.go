// Copyright 2019 Istio Authors. All Rights Reserved.
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

package outboundtrafficpolicy

import (
	"bytes"
	"fmt"
	"html/template"
	"reflect"
	"testing"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	ServiceEntry = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: https-service
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: https
    number: 90
    protocol: HTTPS
  resolution: DNS
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: http
spec:
  hosts:
  - istio.io
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: DNS
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: http-on-443
spec:
  hosts:
  - c.istio.io
  location: MESH_EXTERNAL
  ports:
  - name: http
    number: 443
    protocol: HTTP
  resolution: DNS
`
	SidecarScope = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: restrict-to-service-entry-namespace
spec:
  egress:
  - hosts:
    - "{{.ImportNamespace}}/*"
    - "istio-system/*"
`

	Gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: istio-egressgateway
spec:
  selector:
    istio: egressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "some-external-site.com"
  - port:
      number: 443
      name: https
      protocol: TLS
    hosts:
    - "some-external-site.com"
    tls:
      mode: PASSTHROUGH

---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-via-egressgateway
spec:
  hosts:
    - "some-external-site.com"
  gateways:
  - istio-egressgateway
  - mesh
  http:
    - match:
      - gateways:
        - mesh # from sidecars, route to egress gateway service
        port: 80
      route:
      - destination:
          host: istio-egressgateway.istio-system.svc.cluster.local
          port:
            number: 80
        weight: 100
    - match:
      - gateways:
        - istio-egressgateway
        port: 80
      route:
      - destination:
          host: destination.{{.AppNamespace}}.svc.cluster.local
          port:
            number: 80
        weight: 100
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
    - match:
      - gateways:
        - mesh # from sidecars, route to egress gateway service
        port: 443
      route:
      - destination:
          host: istio-egressgateway.istio-system.svc.cluster.local
          port:
            number: 443
        weight: 100
    - match:
      - gateways:
        - istio-egressgateway
        port: 443
      route:
      - destination:
          host: destination.{{.AppNamespace}}.svc.cluster.local
          port:
            number: 443
        weight: 100
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
`
)

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createSidecarScope(t *testing.T, appsNamespace namespace.Instance, serviceNamespace namespace.Instance, g galley.Instance) {
	tmpl, err := template.New("SidecarScope").Parse(SidecarScope)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ImportNamespace": serviceNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(appsNamespace, buf.String()); err != nil {
		t.Errorf("failed to apply service entries: %v", err)
	}
}

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createGateway(t *testing.T, appsNamespace namespace.Instance, serviceNamespace namespace.Instance, g galley.Instance) {
	tmpl, err := template.New("Gateway").Parse(Gateway)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"AppNamespace": appsNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(serviceNamespace, buf.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, buf.String())
	}
}

// TODO support native environment for registry only/gateway. Blocked by #13177 because the listeners for native use static
// routes and this test relies on the dynamic routes sent through pilot to allow external traffic.

// Expected is a map of protocol -> expected response codes
func RunExternalRequestTest(expected map[string][]string, t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "app",
				Inject: true,
			})
			serviceNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "service",
				Inject: true,
			})

			var client, dest echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&client, echo.Config{
					Service:   "client",
					Namespace: appsNamespace,
					Subsets:   []echo.SubsetConfig{{}},
					Pilot:     p,
					Galley:    g,
				}).
				With(&dest, echo.Config{
					Service:   "destination",
					Namespace: appsNamespace,
					Subsets:   []echo.SubsetConfig{{}},
					Pilot:     p,
					Galley:    g,
					Ports: []echo.Port{
						{
							Name:     "http",
							Protocol: protocol.HTTP,
							// We use a port > 1024 to not require root
							InstancePort: 8090,
						},
						{
							Name:     "https",
							Protocol: protocol.HTTPS,
							// We use a port > 1024 to not require root
							InstancePort: 8091,
						},
					},
				}).BuildOrFail(t)

			// External traffic should work even if we have service entries on the same ports
			createSidecarScope(t, appsNamespace, serviceNamespace, g)
			if err := g.ApplyConfig(serviceNamespace, ServiceEntry); err != nil {
				t.Errorf("failed to apply service entries: %v", err)
			}

			if _, kube := ctx.Environment().(*kube.Environment); kube {
				createGateway(t, appsNamespace, serviceNamespace, g)
			}
			if err := WaitUntilNotCallable(client, dest); err != nil {
				t.Fatalf("failed to apply sidecar, %v", err)
			}

			cases := []struct {
				name     string
				portName string
				host     string
				gateway  bool
			}{
				{
					name:     "HTTP Traffic",
					portName: "http",
				},
				{
					name:     "HTTPS Traffic",
					portName: "https",
				},
				{
					name:     "HTTP Traffic Egress",
					portName: "http",
					host:     "some-external-site.com",
					gateway:  true,
				},
				// TODO add HTTPS through gateway
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					if _, kube := ctx.Environment().(*kube.Environment); !kube && tc.gateway {
						t.Skip("Cannot run gateway in native environment.")
					}
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := client.Call(echo.CallOptions{
							Target:   dest,
							PortName: tc.portName,
							Scheme:   scheme.HTTP,
							Headers: map[string][]string{
								"Host": {tc.host},
							},
						})

						key := tc.portName
						if tc.gateway {
							key += "_egress"
						}
						if err != nil && len(expected[key]) != 0 {
							return fmt.Errorf("request failed: %v", err)
						}
						codes := make([]string, 0, len(resp))
						for _, r := range resp {
							codes = append(codes, r.Code)
						}
						if !reflect.DeepEqual(codes, expected[key]) {
							return fmt.Errorf("got codes %v, expected %v", codes, expected[key])
						}
						for _, r := range resp {
							if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; tc.gateway && !f {
								return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
							}
						}
						return nil
					}, retry.Delay(time.Second), retry.Timeout(10*time.Second))
				})
			}
		})
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

// Wait for the destination to NOT be callable by the client. This allows us to simulate external traffic.
// This essentially just waits for the Sidecar to be applied, without sleeping.
func WaitUntilNotCallable(c echo.Instance, dest echo.Instance) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		for _, port := range dest.Config().Ports {
			clusterName := clusterName(dest, port)
			// Ensure that we have an outbound configuration for the target port.
			err := validator.NotExists("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", clusterName).Check()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
		}
	}

	return nil
}
