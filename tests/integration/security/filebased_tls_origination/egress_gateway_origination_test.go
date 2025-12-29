//go:build integ

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
	"fmt"
	"net/http"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

// TestEgressGatewayTls brings up an cluster and will ensure that the TLS origination at
// egress gateway allows secure communication between the egress gateway and external workload.
// This test brings up an egress gateway to originate TLS connection. The test will ensure that requests
// are routed securely through the egress gateway and that the TLS origination happens at the gateway.
func TestEgressGatewayTls(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// Apply Egress Gateway for service namespace to originate external traffic

			createGateway(t, t, appNS, serviceNS, inst.Settings().EgressGatewayServiceNamespace,
				inst.Settings().EgressGatewayServiceName, inst.Settings().EgressGatewayIstioLabel)

			if err := WaitUntilNotCallable(internalClient[0], externalService[0]); err != nil {
				t.Fatalf("failed to apply sidecar, %v", err)
			}

			// Set up Host Name
			host := "external-service." + serviceNS.Name() + ".svc.cluster.local"

			testCases := map[string]struct {
				destinationRuleMode string
				code                int
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
					code:                http.StatusOK,
					gateway:             true,
					fakeRootCert:        false,
				},
				// 2. Simple TLS case:
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(originates TLS)
				//      --> externalServer(443 with TLS enforced)

				"SIMPLE TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "SIMPLE",
					code:                http.StatusOK,
					gateway:             true,
					fakeRootCert:        false,
				},
				// 3. No TLS case:
				//    internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//      VS Routing (add Egress Header) --> Egress Gateway(does not originate TLS)
				//      --> externalServer(443 with TLS enforced) request fails as gateway tries plain text only
				"No TLS origination from egress gateway to https endpoint": {
					destinationRuleMode: "DISABLE",
					code:                http.StatusBadRequest,
					gateway:             false, // 400 response will not contain header
				},
				// 5. SIMPLE TLS origination with "fake" root cert::
				//   internalClient ) ---HTTP request (Host: some-external-site.com----> Hits listener 0.0.0.0_80 ->
				//     VS Routing (add Egress Header) --> Egress Gateway(originates simple TLS)
				//     --> externalServer(443 with TLS enforced)
				//    request fails as the server cert can't be validated using the fake root cert used during origination
				"SIMPLE TLS origination from egress gateway to https endpoint with fake root cert": {
					destinationRuleMode: "SIMPLE",
					code:                http.StatusServiceUnavailable,
					gateway:             false, // 503 response will not contain header
					fakeRootCert:        true,
				},
			}

			for name, tc := range testCases {
				t.NewSubTest(name).
					Run(func(t framework.TestContext) {
						createDestinationRule(t, serviceNS, tc.destinationRuleMode, tc.fakeRootCert)

						opts := echo.CallOptions{
							To:    externalService[0],
							Count: 1,
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Headers: headers.New().WithHost(host).Build(),
							},
							Retry: echo.Retry{
								Options: []retry.Option{retry.Delay(1 * time.Second), retry.Timeout(2 * time.Minute)},
							},
							Check: check.And(
								check.NoError(),
								check.Status(tc.code),
								check.Each(func(r echoClient.Response) error {
									if _, f := r.RequestHeaders["Handled-By-Egress-Gateway"]; tc.gateway && !f {
										return fmt.Errorf("expected to be handled by gateway. response: %s", r)
									}
									return nil
								})),
						}

						internalClient[0].CallOrFail(t, opts)
					})
			}
		})
}

const (
	// Destination Rule configs
	DestinationRuleConfigSimple = `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: originate-tls-for-server-filebased-simple
spec:
  host: "external-service.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          caCertificates: {{.RootCertPath}}
          sni: external-service.{{.AppNamespace}}.svc.cluster.local

`
	// Destination Rule configs
	DestinationRuleConfigDisabledOrIstioMutual = `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: originate-tls-for-server-filebased-disabled
spec:
  host: "external-service.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          sni: external-service.{{.AppNamespace}}.svc.cluster.local

`
	DestinationRuleConfigMutual = `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: originate-tls-for-server-filebased-mutual
spec:
  host: "external-service.{{.AppNamespace}}.svc.cluster.local"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          clientCertificate: /etc/certs/custom/cert-chain.pem
          privateKey: /etc/certs/custom/key.pem
          caCertificates: {{.RootCertPath}}
          sni: external-service.{{.AppNamespace}}.svc.cluster.local
`
)

func createDestinationRule(t framework.TestContext, serviceNamespace namespace.Instance,
	destinationRuleMode string, fakeRootCert bool,
) {
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
	istioCfg := istio.DefaultConfigOrFail(t, t)
	systemNamespace := namespace.ClaimOrFail(t, istioCfg.SystemNamespace)
	args := map[string]string{
		"AppNamespace": serviceNamespace.Name(),
		"Mode":         destinationRuleMode, "RootCertPath": rootCertPathToUse,
	}
	t.ConfigIstio().Eval(systemNamespace.Name(), args, destinationRuleToParse).ApplyOrFail(t)
}

const (
	Gateway = `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: istio-egressgateway-filebased
spec:
  selector:
    istio: {{.EgressLabel}}
  servers:
    - port:
        number: 443
        name: https-filebased
        protocol: HTTPS
      hosts:
        - external-service.{{.ServerNamespace}}.svc.cluster.local
      tls:
        mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: egressgateway-for-server-filebased
spec:
  host: {{.EgressService}}.{{.EgressNamespace}}.svc.cluster.local
  subsets:
  - name: server
    trafficPolicy:
      portLevelSettings:
      - port:
          number: 443
        tls:
          mode: ISTIO_MUTUAL
          sni: external-service.{{.ServerNamespace}}.svc.cluster.local
`
	VirtualService = `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: route-via-egressgateway-filebased
spec:
  hosts:
    - external-service.{{.ServerNamespace}}.svc.cluster.local
  gateways:
    - istio-egressgateway-filebased
    - mesh
  http:
    - match:
        - gateways:
            - mesh # from sidecars, route to egress gateway service
          port: 80
      route:
        - destination:
            host: {{.EgressService}}.{{.EgressNamespace}}.svc.cluster.local
            subset: server
            port:
              number: 443
          weight: 100
    - match:
        - gateways:
            - istio-egressgateway-filebased
          port: 443
      route:
        - destination:
            host: external-service.{{.ServerNamespace}}.svc.cluster.local
            port:
              number: 443
          weight: 100
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
`
)

func createGateway(t test.Failer, ctx resource.Context, appsNamespace namespace.Instance,
	serviceNamespace namespace.Instance, egressNs string, egressSvc string, egressLabel string,
) {
	ctx.ConfigIstio().
		Eval(appsNamespace.Name(), map[string]string{
			"ServerNamespace": serviceNamespace.Name(),
			"EgressNamespace": egressNs, "EgressLabel": egressLabel, "EgressService": egressSvc,
		}, Gateway).
		Eval(appsNamespace.Name(), map[string]string{
			"ServerNamespace": serviceNamespace.Name(),
			"EgressNamespace": egressNs, "EgressService": egressSvc,
		}, VirtualService).
		ApplyOrFail(t)
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

// Wait for the server to NOT be callable by the client. This allows us to simulate external traffic.
// This essentially just waits for the Sidecar to be applied, without sleeping.
func WaitUntilNotCallable(c echo.Instance, dest echo.Instance) error {
	accept := func(cfg *admin.ConfigDump) (bool, error) {
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
