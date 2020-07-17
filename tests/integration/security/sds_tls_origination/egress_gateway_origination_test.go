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

package sdstlsorigination

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"

	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	sdstlsutil "istio.io/istio/tests/integration/security/sds_tls_origination/util"
)

// TestSimpleTlsOrigination test SIMPLE TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestSimpleTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.tls.sds").
		Run(func(ctx framework.TestContext) {

			var (
				credName        = "tls-credential-cacert"
				fakeCredName    = "fake-tls-credential-cacert"
				credNameMissing = "tls-credential-not-created-cacert"
			)

			var credentialA = sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			var CredentialB = sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.FakeRoot,
			}
			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{credName}, "SIMPLE", credentialA, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{credName})

			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{fakeCredName}, "SIMPLE", CredentialB, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{fakeCredName})

			internalClient, externalServer, _, serverNamespace := sdstlsutil.SetupEcho(t, ctx)

			// Set up Host Namespace
			host := "server." + serverNamespace.Name() + ".svc.cluster.local"

			testCases := map[string]struct {
				response        []string
				credentialToUse string
				gateway         bool // true if the request is expected to be routed through gateway
			}{
				// Use CA certificate stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance.
				"Simple TLS with Correct Root Cert": {
					response:        []string{response.StatusCodeOK},
					credentialToUse: strings.TrimSuffix(credName, "-cacert"),
					gateway:         true,
				},
				// Use CA certificate stored as k8s secret with different issuing CA as server's CA.
				// This root certificate cannot validate the server cert presented by the echoboot server instance.
				"Simple TLS with Fake Root Cert": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(fakeCredName, "-cacert"),
					gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				"Simple TLS with credentialName set when the underlying secret doesn't exist": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					gateway:         false,
				},
			}

			for name, tc := range testCases {
				t.Run(name, func(t *testing.T) {
					bufDestinationRule := sdstlsutil.CreateDestinationRule(t, serverNamespace, "SIMPLE", tc.credentialToUse)

					// Get namespace for gateway pod.
					istioCfg := istio.DefaultConfigOrFail(t, ctx)
					systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

					ctx.Config().ApplyYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())
					defer ctx.Config().DeleteYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())

					retry.UntilSuccessOrFail(t, func() error {
						resp, err := internalClient.Call(echo.CallOptions{
							Target:   externalServer,
							PortName: "http",
							Headers: map[string][]string{
								"Host": {host},
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
					}, retry.Delay(time.Second*1), retry.Timeout(time.Minute*2))
				})
			}
		})
}

// TestMutualTlsOrigination test MUTUAL TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestMutualTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.mtls.sds").
		Run(func(ctx framework.TestContext) {

			var (
				credNameGeneric    = "mtls-credential-generic"
				credNameNotGeneric = "mtls-credential-not-generic"
				fakeCredNameA      = "fake-mtls-credential-a"
				fakeCredNameB      = "fake-mtls-credential-b"
				credNameMissing    = "mtls-credential-not-created"
				simpleCredName     = "tls-credential-simple-cacert"
			)

			var credentialASimple = sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}

			var credentialAGeneric = sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.MustReadCert(t, "cert-chain.pem"),
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}

			var credentialANonGeneric = sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.MustReadCert(t, "cert-chain.pem"),
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Configured with an invalid ClientCert
			var credentialBCert = sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.FakeCert,
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Configured with an invalid ClientCert and PrivateKey
			var credentialBCertAndKey = sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.FakeCert,
				PrivateKey: sdstlsutil.FakeKey,
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{credNameGeneric}, "MUTUAL", credentialAGeneric, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{credNameGeneric})

			sdstlsutil.CreateKubeSecret(t, ctx, []string{credNameNotGeneric}, "MUTUAL", credentialANonGeneric, true)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{credNameNotGeneric})

			sdstlsutil.CreateKubeSecret(t, ctx, []string{fakeCredNameA}, "MUTUAL", credentialBCert, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{fakeCredNameA})

			sdstlsutil.CreateKubeSecret(t, ctx, []string{fakeCredNameB}, "MUTUAL", credentialBCertAndKey, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{fakeCredNameB})

			sdstlsutil.CreateKubeSecret(t, ctx, []string{simpleCredName}, "SIMPLE", credentialASimple, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{simpleCredName})

			internalClient, externalServer, _, serverNamespace := sdstlsutil.SetupEcho(t, ctx)

			// Set up Host Namespace
			host := "server." + serverNamespace.Name() + ".svc.cluster.local"

			testCases := map[string]struct {
				response        []string
				credentialToUse string
				gateway         bool // true if the request is expected to be routed through gateway
			}{
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is of type generic.
				"MUTUAL TLS with correct root cert and client certs and generic secret type": {
					response:        []string{response.StatusCodeOK},
					credentialToUse: strings.TrimSuffix(credNameGeneric, "-cacert"),
					gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is not of type generic.
				"MUTUAL TLS with correct root cert and client certs and non generic secret type": {
					response:        []string{response.StatusCodeOK},
					credentialToUse: strings.TrimSuffix(credNameNotGeneric, "-cacert"),
					gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA
				// cannot validate the client cert. Returns 503 response as TLS handshake fails.
				"MUTUAL TLS with correct root cert but invalid client cert": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(fakeCredNameA, "-cacert"),
					gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				"MUTUAL TLS with credentialName set when the underlying secret doesn't exist": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					gateway:         false,
				},
				"MUTUAL TLS with correct root cert but no client certs": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(simpleCredName, "-cacert"),
					gateway:         false,
				},
			}

			for name, tc := range testCases {
				ctx.NewSubTest(name).
					Run(func(ctx framework.TestContext) {
						bufDestinationRule := sdstlsutil.CreateDestinationRule(t, serverNamespace, "MUTUAL", tc.credentialToUse)

						// Get namespace for gateway pod.
						istioCfg := istio.DefaultConfigOrFail(t, ctx)
						systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

						ctx.Config().ApplyYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())
						defer ctx.Config().DeleteYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())

						retry.UntilSuccessOrFail(t, func() error {
							resp, err := internalClient.Call(echo.CallOptions{
								Target:   externalServer,
								PortName: "http",
								Headers: map[string][]string{
									"Host": {host},
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
						}, retry.Delay(time.Second*1), retry.Timeout(time.Minute*2))
					})
			}
		})
}
