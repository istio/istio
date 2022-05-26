//go:build integ
// +build integ

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

package security

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
	sdstlsutil "istio.io/istio/tests/integration/security/sds_tls_origination/util"
)

// TestSimpleTlsOrigination test SIMPLE TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestSimpleTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleNetwork(). // https://github.com/istio/istio/issues/37134
		Features("security.egress.tls.sds").
		Run(func(t framework.TestContext) {
			var (
				credName        = "tls-credential-cacert"
				fakeCredName    = "fake-tls-credential-cacert"
				credNameMissing = "tls-credential-not-created-cacert"
			)

			credentialA := ingressutil.IngressCredential{
				CaCert: MustReadCert(t, "root-cert.pem"),
			}
			CredentialB := ingressutil.IngressCredential{
				CaCert: sdstlsutil.FakeRoot,
			}
			// Add kubernetes secret to provision key/cert for gateway.
			ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.TLS, credentialA, false)

			// Add kubernetes secret to provision key/cert for gateway.
			ingressutil.CreateIngressKubeSecret(t, fakeCredName, ingressutil.TLS, CredentialB, false)

			// Set up Host Namespace
			host := apps.External.Config().ClusterLocalFQDN()

			testCases := []TLSTestCase{
				// Use CA certificate stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance.
				{
					Name:            "simple",
					StatusCode:      http.StatusOK,
					CredentialToUse: strings.TrimSuffix(credName, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate stored as k8s secret with different issuing CA as server's CA.
				// This root certificate cannot validate the server cert presented by the echoboot server instance.
				{
					Name:            "fake root",
					StatusCode:      http.StatusServiceUnavailable,
					CredentialToUse: strings.TrimSuffix(fakeCredName, "-cacert"),
					Gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				{
					Name:            "missing secret",
					StatusCode:      http.StatusServiceUnavailable,
					CredentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					Gateway:         false,
				},
			}

			CreateGateway(t, t, apps.Namespace1, apps.External)
			for _, tc := range testCases {
				t.NewSubTest(tc.Name).Run(func(t framework.TestContext) {
					CreateDestinationRule(t, apps.External, "SIMPLE", tc.CredentialToUse)
					newGatewayTest(t).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							callOpt := CallOpts(to, host, tc)
							from.CallOrFail(t, callOpt)
						})
				})
			}
		})
}

// TestMutualTlsOrigination test MUTUAL TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestMutualTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleNetwork(). // https://github.com/istio/istio/issues/37134
		Features("security.egress.mtls.sds").
		Run(func(t framework.TestContext) {
			var (
				credNameGeneric    = "mtls-credential-generic"
				credNameNotGeneric = "mtls-credential-not-generic"
				fakeCredNameA      = "fake-mtls-credential-a"
				fakeCredNameB      = "fake-mtls-credential-b"
				credNameMissing    = "mtls-credential-not-created"
				simpleCredName     = "tls-credential-simple-cacert"
			)

			// Add kubernetes secret to provision key/cert for gateway.

			ingressutil.CreateIngressKubeSecret(t, credNameGeneric, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: MustReadCert(t, "cert-chain.pem"),
				PrivateKey:  MustReadCert(t, "key.pem"),
				CaCert:      MustReadCert(t, "root-cert.pem"),
			}, false)

			ingressutil.CreateIngressKubeSecret(t, credNameNotGeneric, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: MustReadCert(t, "cert-chain.pem"),
				PrivateKey:  MustReadCert(t, "key.pem"),
				CaCert:      MustReadCert(t, "root-cert.pem"),
			}, true)

			// Configured with an invalid ClientCert
			ingressutil.CreateIngressKubeSecret(t, fakeCredNameA, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: sdstlsutil.FakeCert,
				PrivateKey:  MustReadCert(t, "key.pem"),
				CaCert:      MustReadCert(t, "root-cert.pem"),
			}, false)

			// Configured with an invalid ClientCert and PrivateKey
			ingressutil.CreateIngressKubeSecret(t, fakeCredNameB, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: sdstlsutil.FakeCert,
				PrivateKey:  sdstlsutil.FakeKey,
				CaCert:      MustReadCert(t, "root-cert.pem"),
			}, false)

			ingressutil.CreateIngressKubeSecret(t, simpleCredName, ingressutil.TLS, ingressutil.IngressCredential{
				CaCert: MustReadCert(t, "root-cert.pem"),
			}, false)

			// Set up Host Namespace
			host := apps.External.Config().ClusterLocalFQDN()

			testCases := []TLSTestCase{
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is of type generic.
				{
					Name:            "generic",
					StatusCode:      http.StatusOK,
					CredentialToUse: strings.TrimSuffix(credNameGeneric, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is not of type generic.
				{
					Name:            "non-generic",
					StatusCode:      http.StatusOK,
					CredentialToUse: strings.TrimSuffix(credNameNotGeneric, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA
				// cannot validate the client cert. Returns 503 response as TLS handshake fails.
				{
					Name:            "invalid client cert",
					StatusCode:      http.StatusServiceUnavailable,
					CredentialToUse: strings.TrimSuffix(fakeCredNameA, "-cacert"),
					Gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				{
					Name:            "missing",
					StatusCode:      http.StatusServiceUnavailable,
					CredentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					Gateway:         false,
				},
				{
					Name:            "no client certs",
					StatusCode:      http.StatusServiceUnavailable,
					CredentialToUse: strings.TrimSuffix(simpleCredName, "-cacert"),
					Gateway:         false,
				},
			}

			CreateGateway(t, t, apps.Namespace1, apps.External)
			for _, tc := range testCases {
				t.NewSubTest(tc.Name).Run(func(t framework.TestContext) {
					CreateDestinationRule(t, apps.External, "MUTUAL", tc.CredentialToUse)
					newGatewayTest(t).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							callOpt := CallOpts(to, host, tc)
							from.CallOrFail(t, callOpt)
						})
				})
			}
		})
}

const (
	Gateway = `
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: istio-egressgateway-sds
spec:
  selector:
    istio: egressgateway
  servers:
    - port:
        number: 443
        name: https-sds
        protocol: HTTPS
      hosts:
      - {{ .to.Config.ClusterLocalFQDN }}
      tls:
        mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: egressgateway-for-server-sds
spec:
  host: istio-egressgateway.istio-system.svc.cluster.local
  subsets:
  - name: server
    trafficPolicy:
      portLevelSettings:
      - port:
          number: 443
        tls:
          mode: ISTIO_MUTUAL
          sni: {{ .to.Config.ClusterLocalFQDN }}
`
	VirtualService = `
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: route-via-egressgateway-sds
spec:
  hosts:
    - {{ .to.Config.ClusterLocalFQDN }}
  gateways:
    - istio-egressgateway-sds
    - mesh
  http:
    - match:
        - gateways:
            - mesh # from sidecars, route to egress gateway service
          port: 80
      route:
        - destination:
            host: istio-egressgateway.istio-system.svc.cluster.local
            subset: server
            port:
              number: 443
          weight: 100
    - match:
        - gateways:
            - istio-egressgateway-sds
          port: 443
      route:
        - destination:
            host: {{ .to.Config.ClusterLocalFQDN }}
            port:
              number: 443
          weight: 100
      headers:
        request:
          add:
            handled-by-egress-gateway: "true"
`
)

// We want to test out TLS origination at Gateway, to do so traffic from client in client namespace is first
// routed to egress-gateway service in istio-system namespace and then from egress-gateway to server in server namespace.
// TLS origination at Gateway happens using DestinationRule with CredentialName reading k8s secret at the gateway proxy.
func CreateGateway(t test.Failer, ctx resource.Context, clientNamespace namespace.Instance, to echo.Instances) {
	args := map[string]interface{}{"to": to}

	ctx.ConfigIstio().Eval(clientNamespace.Name(), args, Gateway, VirtualService).ApplyOrFail(t)
}

const (
	// Destination Rule configs
	DestinationRuleConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server-sds-{{.CredentialName}}
spec:
  host: "{{ .to.Config.ClusterLocalFQDN }}"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 443
        tls:
          mode: {{.Mode}}
          credentialName: {{.CredentialName}}
          sni: {{ .to.Config.ClusterLocalFQDN }}
`
)

// Create the DestinationRule for TLS origination at Gateway by reading secret in istio-system namespace.
func CreateDestinationRule(t framework.TestContext, to echo.Instances,
	destinationRuleMode string, credentialName string,
) {
	args := map[string]interface{}{
		"to":             to,
		"Mode":           destinationRuleMode,
		"CredentialName": credentialName,
	}

	// Get namespace for gateway pod.
	istioCfg := istio.DefaultConfigOrFail(t, t)
	systemNS := namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

	t.ConfigKube(t.Clusters().Default()).Eval(systemNS.Name(), args, DestinationRuleConfig).
		ApplyOrFail(t)
}

type TLSTestCase struct {
	Name            string
	StatusCode      int
	CredentialToUse string
	Gateway         bool // true if the request is expected to be routed through gateway
}

func CallOpts(to echo.Target, host string, tc TLSTestCase) echo.CallOptions {
	return echo.CallOptions{
		To: to,
		Port: echo.Port{
			Name: "http",
		},
		HTTP: echo.HTTP{
			Headers: headers.New().WithHost(host).Build(),
		},
		Check: check.And(
			check.NoErrorAndStatus(tc.StatusCode),
			check.Each(func(r echoClient.Response) error {
				if _, f := r.RequestHeaders["Handled-By-Egress-Gateway"]; tc.Gateway && !f {
					return fmt.Errorf("expected to be handled by gateway. response: %s", r)
				}
				return nil
			})),
	}
}

func MustReadCert(t test.Failer, f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

func newGatewayTest(t framework.TestContext) *echotest.T {
	return echotest.New(t, apps.All).
		WithDefaultFilters().
		FromMatch(match.And(
			match.NotNaked,
			match.Or(
				match.ServiceName(apps.A.NamespacedName()),
				match.Not(match.RegularPod)))).
		ToMatch(match.ServiceName(apps.External.NamespacedName()))
}
