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

package sdstlsorigination

import (
	"strings"
	"testing"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
	sdstlsutil "istio.io/istio/tests/integration/security/sds_tls_origination/util"
)

// TestSimpleTlsOrigination test SIMPLE TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestSimpleTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.tls.sds").
		Run(func(t framework.TestContext) {
			var (
				credName        = "tls-credential-cacert"
				fakeCredName    = "fake-tls-credential-cacert"
				credNameMissing = "tls-credential-not-created-cacert"
			)

			credentialA := sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			CredentialB := sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.FakeRoot,
			}
			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, []string{credName}, "SIMPLE", credentialA, false)
			defer ingressutil.DeleteKubeSecret(t, []string{credName})

			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, []string{fakeCredName}, "SIMPLE", CredentialB, false)
			defer ingressutil.DeleteKubeSecret(t, []string{fakeCredName})

			apps := &sdstlsutil.EchoDeployments{}
			sdstlsutil.SetupEcho(t, t, apps)

			// Set up Host Namespace
			host := sdstlsutil.ServerSvc + "." + apps.ServerNamespace.Name() + ".svc.cluster.local"

			testCases := map[string]sdstlsutil.TestCase{
				// Use CA certificate stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance.
				"Simple TLS with Correct Root Cert": {
					Response:        response.StatusCodeOK,
					CredentialToUse: strings.TrimSuffix(credName, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate stored as k8s secret with different issuing CA as server's CA.
				// This root certificate cannot validate the server cert presented by the echoboot server instance.
				"Simple TLS with Fake Root Cert": {
					Response:        response.StatusCodeUnavailable,
					CredentialToUse: strings.TrimSuffix(fakeCredName, "-cacert"),
					Gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				"Simple TLS with credentialName set when the underlying secret doesn't exist": {
					Response:        response.StatusCodeUnavailable,
					CredentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					Gateway:         false,
				},
			}

			for name, tc := range testCases {
				echotest.New(t, apps.All).
					SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
						bufDestinationRule := sdstlsutil.CreateDestinationRule(t, apps.ServerNamespace, "SIMPLE", tc.CredentialToUse)

						// Get namespace for gateway pod.
						istioCfg := istio.DefaultConfigOrFail(t, t)
						systemNS := namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

						t.Config(t.Clusters().Default()).ApplyYAMLOrFail(t, systemNS.Name(), bufDestinationRule.String())
						return nil
					}).
					From(
						echotest.Not(func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.Service(sdstlsutil.ServerSvc))
						}),
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.InCluster(t.Clusters().Default()))
						},
					).
					ConditionallyTo(echotest.ReachableDestinations).
					To(
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.Service(sdstlsutil.ServerSvc))
						},
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.InCluster(t.Clusters().Default()))
						},
					).
					Run(func(t framework.TestContext, src echo.Instance, _ echo.Instances) {
						callOpt := sdstlsutil.CallOpts(apps.Server, host, name, tc)
						t.NewSubTest(name).Run(func(t framework.TestContext) {
							src.CallWithRetryOrFail(t, callOpt, echo.DefaultCallRetryOptions()...)
						})
					})
			}
		})
}

// TestMutualTlsOrigination test MUTUAL TLS mode with TLS origination happening at Gateway proxy
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestMutualTlsOrigination(t *testing.T) {
	framework.NewTest(t).
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

			credentialASimple := sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}

			credentialAGeneric := sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.MustReadCert(t, "cert-chain.pem"),
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}

			credentialANonGeneric := sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.MustReadCert(t, "cert-chain.pem"),
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Configured with an invalid ClientCert
			credentialBCert := sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.FakeCert,
				PrivateKey: sdstlsutil.MustReadCert(t, "key.pem"),
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Configured with an invalid ClientCert and PrivateKey
			credentialBCertAndKey := sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.FakeCert,
				PrivateKey: sdstlsutil.FakeKey,
				CaCert:     sdstlsutil.MustReadCert(t, "root-cert.pem"),
			}
			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, []string{credNameGeneric}, "MUTUAL", credentialAGeneric, false)
			defer ingressutil.DeleteKubeSecret(t, []string{credNameGeneric})

			sdstlsutil.CreateKubeSecret(t, []string{credNameNotGeneric}, "MUTUAL", credentialANonGeneric, true)
			defer ingressutil.DeleteKubeSecret(t, []string{credNameNotGeneric})

			sdstlsutil.CreateKubeSecret(t, []string{fakeCredNameA}, "MUTUAL", credentialBCert, false)
			defer ingressutil.DeleteKubeSecret(t, []string{fakeCredNameA})

			sdstlsutil.CreateKubeSecret(t, []string{fakeCredNameB}, "MUTUAL", credentialBCertAndKey, false)
			defer ingressutil.DeleteKubeSecret(t, []string{fakeCredNameB})

			sdstlsutil.CreateKubeSecret(t, []string{simpleCredName}, "SIMPLE", credentialASimple, false)
			defer ingressutil.DeleteKubeSecret(t, []string{simpleCredName})

			apps := &sdstlsutil.EchoDeployments{}
			sdstlsutil.SetupEcho(t, t, apps)

			// Set up Host Namespace
			host := sdstlsutil.ServerSvc + "." + apps.ServerNamespace.Name() + ".svc.cluster.local"

			testCases := map[string]sdstlsutil.TestCase{
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is of type generic.
				"MUTUAL TLS with correct root cert and client certs and generic secret type": {
					Response:        response.StatusCodeOK,
					CredentialToUse: strings.TrimSuffix(credNameGeneric, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA can
				// validate the client cert. Secret is not of type generic.
				"MUTUAL TLS with correct root cert and client certs and non generic secret type": {
					Response:        response.StatusCodeOK,
					CredentialToUse: strings.TrimSuffix(credNameNotGeneric, "-cacert"),
					Gateway:         true,
				},
				// Use CA certificate and client certs stored as k8s secret with the same issuing CA as server's CA.
				// This root certificate can validate the server cert presented by the echoboot server instance and server CA
				// cannot validate the client cert. Returns 503 response as TLS handshake fails.
				"MUTUAL TLS with correct root cert but invalid client cert": {
					Response:        response.StatusCodeUnavailable,
					CredentialToUse: strings.TrimSuffix(fakeCredNameA, "-cacert"),
					Gateway:         false,
				},

				// Set up an UpstreamCluster with a CredentialName when secret doesn't even exist in istio-system ns.
				// Secret fetching error at Gateway, results in a 503 response.
				"MUTUAL TLS with credentialName set when the underlying secret doesn't exist": {
					Response:        response.StatusCodeUnavailable,
					CredentialToUse: strings.TrimSuffix(credNameMissing, "-cacert"),
					Gateway:         false,
				},
				"MUTUAL TLS with correct root cert but no client certs": {
					Response:        response.StatusCodeUnavailable,
					CredentialToUse: strings.TrimSuffix(simpleCredName, "-cacert"),
					Gateway:         false,
				},
			}

			for name, tc := range testCases {
				echotest.New(t, apps.All).
					SetupForDestination(func(t framework.TestContext, dst echo.Instances) error {
						bufDestinationRule := sdstlsutil.CreateDestinationRule(t, apps.ServerNamespace, "MUTUAL", tc.CredentialToUse)

						// Get namespace for gateway pod.
						istioCfg := istio.DefaultConfigOrFail(t, t)
						systemNS := namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

						t.Config(t.Clusters().Default()).ApplyYAMLOrFail(t, systemNS.Name(), bufDestinationRule.String())
						return nil
					}).
					From(
						echotest.Not(func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.Service(sdstlsutil.ServerSvc))
						}),
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.InCluster(t.Clusters().Default()))
						},
					).
					ConditionallyTo(echotest.ReachableDestinations).
					To(
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.Service(sdstlsutil.ServerSvc))
						},
						func(instances echo.Instances) echo.Instances {
							return instances.Match(echo.InCluster(t.Clusters().Default()))
						},
					).
					Run(func(t framework.TestContext, src echo.Instance, _ echo.Instances) {
						callOpt := sdstlsutil.CallOpts(apps.Server, host, name, tc)
						t.NewSubTest(name).Run(func(t framework.TestContext) {
							src.CallWithRetryOrFail(t, callOpt, echo.DefaultCallRetryOptions()...)
						})
					})
			}
		})
}
