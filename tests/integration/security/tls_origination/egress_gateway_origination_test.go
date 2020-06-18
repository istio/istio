//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package egressgatewayorigination

import (
	"bytes"
	"fmt"
	"html/template"
	"path"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/resource"

	"io/ioutil"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

func mustReadCert(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

const (
	// paths to test configs
	simpleTLSDestinationRuleConfig  = "testdata/destination-rule-tls-origination.yaml"
	mutualTLSDestinationRuleConfig  = "testdata/destination-rule-mtls-origination.yaml"
	disableTLSDestinationRuleConfig = "testdata/destination-rule-no-tls-origination.yaml"
)

// TestEgressGatewayTls brings up an cluster and will ensure that the TLS origination at
// egress gateway allows secure communication between the egress gateway and external workload.
// This test brings up an egress gateway to originate TLS connection. The test will ensure that requests
// are routed securely through the egress gateway and that the TLS origination happens at the gateway.
func TestEgressGatewayTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.tls").
		Run(func(ctx framework.TestContext) {
			ctx.RequireOrSkip(environment.Kube)

			client, server, _, serviceNamespace := setupEcho(t, ctx)

			testCases := map[string]struct {
				destinationRulePath string
				response            []string
				portName            string
			}{
				"SIMPLE TLS origination from egress gateway succeeds": {
					destinationRulePath: simpleTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "https",
				},
				"TLS origination from egress gateway to http endpoint": {
					destinationRulePath: simpleTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "http",
				},
				"No TLS origination from egress gateway to https endpoint": {
					destinationRulePath: disableTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "https",
				},
				"No TLS origination from egress gateway to http endpoint": {
					destinationRulePath: disableTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "http",
				},
				"Mutual TLS origination from egress gateway to https endpoint no certs": {
					destinationRulePath: mutualTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "https",
				},
			}

			for name, tc := range testCases {
				t.Run(name, func(t *testing.T) {
					ctx.ApplyConfigOrFail(ctx, serviceNamespace.Name(), file.AsStringOrFail(ctx, tc.destinationRulePath))
					defer ctx.DeleteConfigOrFail(ctx, serviceNamespace.Name(), file.AsStringOrFail(ctx, tc.destinationRulePath))

					retry.UntilSuccessOrFail(t, func() error {
						resp, err := client.Call(echo.CallOptions{
							Target:   server,
							PortName: tc.portName,
							Headers: map[string][]string{
								"Host": {"some-external-site.com"},
							},
						})
						if err != nil {
							return fmt.Errorf("request failed: %v", err)
						}
						codes := make([]string, 0, len(resp))
						for _, r := range resp {
							codes = append(codes, r.Code)
						}
						if !reflect.DeepEqual(codes, tc.response) {
							return fmt.Errorf("got codes %q, expected %q", codes, tc.response)
						}
						for _, r := range resp {
							if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; !f {
								return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
							}
						}
						return nil
					}, retry.Delay(time.Second), retry.Timeout(20*time.Second))
				})
			}
		})
}

// setupEcho creates two namespaces app and service. It also brings up two echo instances server and
// client in app namespace. HTTP and HTTPS port on the server echo are set up. Sidecar scope config
// is applied to only allow egress traffic to service namespace such that when client to server calls are made
// we are able to simulate "external" traffic by going outside this namespace. Egress Gateway is set up in the
// service namespace to handle egress for "external" calls.
func setupEcho(t *testing.T, ctx resource.Context) (echo.Instance, echo.Instance, namespace.Instance, namespace.Instance) {
	appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})
	serviceNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "service",
		Inject: true,
	})

	var client, server echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: appsNamespace,
			Pilot:     p,
			Subsets:   []echo.SubsetConfig{{}},
		}).
		With(&server, echo.Config{
			Service:   "destination",
			Namespace: appsNamespace,
			Ports: []echo.Port{
				{
					// Plain HTTP port
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					InstancePort: 8080,
					TLS:          false, // default
				},
				{
					// HTTPS port
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			Pilot: p,
			// Set up TLS certs on the server. This will make the server listen with these credentials.
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				RootCert:   mustReadCert(t, "cacert.pem"),
				ClientCert: mustReadCert(t, "cert.crt"),
				Key:        mustReadCert(t, "cert.key"),
			},
		}).
		BuildOrFail(t)

	// External traffic should work even if we have service entries on the same ports
	// Only Service namespace is known so that app namespace "appears" to be outside the mesh
	createSidecarScope(t, ctx, appsNamespace, serviceNamespace)

	// Apply Egress Gateway for service namespace to handle external traffic
	createGateway(t, ctx, appsNamespace, serviceNamespace)

	if err := WaitUntilNotCallable(client, server); err != nil {
		t.Fatalf("failed to apply sidecar, %v", err)
	}

	return client, server, appsNamespace, serviceNamespace
}

const (
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
  outboundTrafficPolicy:
    mode: ALLOW_ANY
`
)

// We want to test "external" traffic. To do this without actually hitting an external endpoint,
// we can import only the service namespace, so the apps are not known
func createSidecarScope(t *testing.T, ctx resource.Context, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	tmpl, err := template.New("SidecarScope").Parse(SidecarScope)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ImportNamespace": serviceNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := ctx.ApplyConfig(appsNamespace.Name(), buf.String()); err != nil {
		t.Errorf("failed to apply sidecar scope: %v", err)
	}
}

const (
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
        name: http-port-for-tls-origination
        protocol: HTTP
      hosts:
        - some-external-site.com
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
            - mesh # from sidecars, route to egress gateway service
          port: 443
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
func createGateway(t *testing.T, ctx resource.Context, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	tmpl, err := template.New("Gateway").Parse(Gateway)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"AppNamespace": appsNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := ctx.ApplyConfig(serviceNamespace.Name(), buf.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, buf.String())
	}
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
			err := validator.NotExists("{.configs[*].dynamicActiveClusters[?(@.cluster.Name == '%s')]}", clusterName).Check()
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
