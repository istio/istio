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

package filebasedtlsorigination

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path"
	"reflect"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/resource"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

func mustReadCert(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// TestEgressGatewayTls brings up an cluster and will ensure that the TLS origination at
// egress gateway allows secure communication between the egress gateway and external workload.
// This test brings up an egress gateway to originate TLS connection. The test will ensure that requests
// are routed securely through the egress gateway and that the TLS origination happens at the gateway.
func TestEgressGatewayTls(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/25069")
	framework.NewTest(t).
		Features("security.egress.tls.filebased").
		Run(func(ctx framework.TestContext) {
			internalClient, externalServer, appNamespace, _ := setupEcho(t, ctx)

			testCases := map[string]struct {
				destinationRuleMode string
				response            []string
				gateway             bool //  If gateway is true, request is expected to pass through the egress gateway
				fakeRootCert        bool // If Fake root cert is to be used to verify server's presented certificate
			}{
				// Mutual Connection is originated by our DR but server side drops the connection to
				// only use Simple TLS as it doesn't verify client side cert
				// TODO: mechanism to enforce mutual TLS(client cert) validation by the server
				// 1. Mutual TLS origination from egress gateway to https endpoint:
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(originates mTLS with client certs)
				//      --> externalServer(443 with only Simple TLS used and client cert is not verified)
				"Mutual TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "MUTUAL",
					response:            []string{response.StatusCodeOK},
					gateway:             true,
					fakeRootCert:        false,
				},
				// 2. Simple TLS case:
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(originates TLS)
				//      --> externalServer(443 with TLS enforced)

				"SIMPLE TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "SIMPLE",
					response:            []string{response.StatusCodeOK},
					gateway:             true,
					fakeRootCert:        false,
				},
				// 3. No TLS case:
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(does not originate TLS)
				//      --> externalServer(443 with TLS enforced) request fails as gateway tries plain text only
				"No TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "DISABLE",
					response:            []string{response.StatusCodeBadRequest},
					gateway:             false, // 400 response will not contain header
				},
				// Egress Gateway can't "originate" ISTIO_MUTUAL to server as the server is outside the mesh and has no
				// sidecar proxy
				// 4. ISTIO_MUTUAL TLS origination case::
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(originates istio mutual TLS)
				//      --> externalServer(443 with TLS enforced)
				//     request fails as server has no sidecar proxy and istio_mutual case shouldn't be used
				// TODO: nschhina figure out why the following tests are flaky
				"ISTIO_MUTUAL TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "ISTIO_MUTUAL",
					response:            []string{response.StatusCodeBadRequest},
					gateway:             false, // 400 response will not contain header
					fakeRootCert:        false,
				},
				//5. SIMPLE TLS origination with "fake" root cert::
				//   internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//     VS Routing (add Egress Header) --> Egress Gateway(originates simple TLS)
				//     --> externalServer(443 with TLS enforced)
				//    request fails as the server cert can't be validated using the fake root cert used during origination
				"SIMPLE TLS origination from egress gateway to https endpoint with fake root cert": {
					destinationRuleMode: "SIMPLE",
					response:            []string{response.StatusCodeBadRequest},
					gateway:             false, // 400 response will not contain header
					fakeRootCert:        true,
				},
			}

			for name, tc := range testCases {
				t.Run(name, func(t *testing.T) {
					bufDestinationRule := createDestinationRule(t, appNamespace, tc.destinationRuleMode, tc.fakeRootCert)

					ctx.Config().ApplyYAMLOrFail(ctx, appNamespace.Name(), bufDestinationRule.String())
					defer ctx.Config().DeleteYAMLOrFail(ctx, appNamespace.Name(), bufDestinationRule.String())

					retry.UntilSuccessOrFail(t, func() error {
						resp, err := internalClient.Call(echo.CallOptions{
							Target:   externalServer,
							PortName: "http",
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
							if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; tc.gateway && !f {
								return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
							}
						}
						return nil
					}, retry.Delay(time.Second), retry.Timeout(5*time.Second))
				})
			}
		})
}

const (
	// Destination Rule configs
	DestinationRuleConfigSimple = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          caCertificates: {{.RootCertPath}}

`
	// Destination Rule configs
	DestinationRuleConfigDisabledOrIstioMutual = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}

`
	DestinationRuleConfigMutual = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-mtls-for-server
spec:
  host: "server.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          clientCertificate: /etc/certs/custom/cert-chain.pem
          privateKey: /etc/certs/custom/key.pem
          caCertificates: {{.RootCertPath}}
`
)

func createDestinationRule(t *testing.T, appsNamespace namespace.Instance,
	destinationRuleMode string, fakeRootCert bool) bytes.Buffer {
	var destinationRuleToParse string
	var rootCertPathToUse string
	if destinationRuleMode == "MUTUAL" {
		destinationRuleToParse = DestinationRuleConfigMutual
	} else if destinationRuleMode == "SIMPLE" {
		destinationRuleToParse = DestinationRuleConfigSimple
	} else {
		destinationRuleToParse = DestinationRuleConfigDisabledOrIstioMutual
	}
	if fakeRootCert {
		rootCertPathToUse = "/etc/certs/custom/fake-root-cert.pem"
	} else {
		rootCertPathToUse = "/etc/certs/custom/root-cert.pem"
	}
	tmpl, err := template.New("DestinationRule").Parse(destinationRuleToParse)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"AppNamespace": appsNamespace.Name(),
		"Mode": destinationRuleMode, "RootCertPath": rootCertPathToUse}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	return buf
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

	var internalClient, externalServer echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&internalClient, echo.Config{
			Service:   "client",
			Namespace: appsNamespace,
			Ports:     []echo.Port{},
			Subsets: []echo.SubsetConfig{{
				Version: "v1",
			}},
		}).
		With(&externalServer, echo.Config{
			Service:   "server",
			Namespace: appsNamespace,
			Ports: []echo.Port{
				{
					// Plain HTTP port only used to route request to egress gateway
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					InstancePort: 8080,
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
			// Set up TLS certs on the server. This will make the server listen with these credentials.
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				RootCert:   mustReadCert(t, "root-cert.pem"),
				ClientCert: mustReadCert(t, "cert-chain.pem"),
				Key:        mustReadCert(t, "key.pem"),
				// Override hostname to match the SAN in the cert we are using
				Hostname: "server.default.svc",
			},
			Subsets: []echo.SubsetConfig{{
				Version:     "v1",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			}},
		}).
		BuildOrFail(t)

	// Create a sidecarScope to only allow traffic to service namespace so that the traffic only goes
	// through the egress gateway set up in service namespace. This way client cannot directly call our server
	// in the same namespace("app") and is forced to route traffic to egress gateway
	createSidecarScope(t, ctx, appsNamespace, serviceNamespace)

	// Apply Egress Gateway for service namespace to originate external traffic
	createGateway(t, ctx, appsNamespace, serviceNamespace)

	if err := WaitUntilNotCallable(internalClient, externalServer); err != nil {
		t.Fatalf("failed to apply sidecar, %v", err)
	}

	return internalClient, externalServer, appsNamespace, serviceNamespace
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
// If some service(client) in appNamespace wants to call another service in appNamespace(server) it cannot
// directly call it as sidecarScope only allows traffic to serviceNamespace
func createSidecarScope(t *testing.T, ctx resource.Context, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	tmpl, err := template.New("SidecarScope").Parse(SidecarScope)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ImportNamespace": serviceNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := ctx.Config().ApplyYAML(appsNamespace.Name(), buf.String()); err != nil {
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
    - some-external-site.com
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
            host: server.{{.AppNamespace}}.svc.cluster.local
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
// we can import only the service namespace, so the apps are not known.
// If some service(client) in appNamespace wants to call another service in appNamespace(server) it cannot
// directly call it as sidecarscope only allows traffic to serviceNamespace, Gateway in serviceNamespace
// then comes into action and receives this "outgoing" traffic to only route it back into appNamespace(server)
func createGateway(t *testing.T, ctx resource.Context, appsNamespace namespace.Instance, serviceNamespace namespace.Instance) {
	tmpl, err := template.New("Gateway").Parse(Gateway)
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"AppNamespace": appsNamespace.Name()}); err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	if err := ctx.Config().ApplyYAML(serviceNamespace.Name(), buf.String()); err != nil {
		t.Fatalf("failed to apply gateway: %v. template: %v", err, buf.String())
	}
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

// Wait for the server to NOT be callable by the client. This allows us to simulate external traffic.
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
