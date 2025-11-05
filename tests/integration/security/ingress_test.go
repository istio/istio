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

package security

import (
	"net/http"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/namespace"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// TestSingleTlsGateway_SecretRotation tests a single TLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) create a kubernetes secret to provision server key/cert, and
// verify that TLS connection could establish to deliver HTTPS request.
// (2) Rotates key/cert by deleting the secret generated in (1) and
// replacing it a new secret with a different server key/cert.
// (3) verify that client using older CA cert gets a 404 response
// (4) verify that client using the newer CA cert is able to establish TLS connection
// to deliver the HTTPS request.
func TestSingleTlsGateway_SecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			var (
				credName = "testsingletlsgateway-secretrotation"
				host     = "testsingletlsgateway-secretrotation.example.com"
			)
			allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
			for _, instances := range allInstances {
				echotest.New(t, instances).
					SetupForDestination(func(t framework.TestContext, to echo.Target) error {
						ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
							Mode:           "SIMPLE",
							CredentialName: credName,
							Host:           host,
							ServiceName:    to.Config().Service,
							GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
						})
						return nil
					}).
					To(echotest.SingleSimplePodServiceAndAllSpecial()).
					RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
						// Add kubernetes secret to provision key/cert for ingress gateway.
						ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.TLS,
							ingressutil.IngressCredentialA, false)

						ing := i.IngressFor(t.Clusters().Default())
						if ing == nil {
							t.Skip()
						}

						tlsContextA := ingressutil.TLSContext{CaCert: ingressutil.CaCertA}
						tlsContextB := ingressutil.TLSContext{CaCert: ingressutil.CaCertB}

						// Verify the call works
						ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.TLS, tlsContextA,
							ingressutil.ExpectedResponse{StatusCode: http.StatusOK})

						// Now rotate the key/cert
						ingressutil.RotateSecrets(t, credName, ingressutil.TLS,
							ingressutil.IngressCredentialB, false)

						t.NewSubTest("old cert should fail").Run(func(t framework.TestContext) {
							// Client use old server CA cert to set up SSL connection would fail.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.TLS, tlsContextA,
								ingressutil.ExpectedResponse{ErrorMessage: "certificate signed by unknown authority"})
						})

						t.NewSubTest("new cert should succeed").Run(func(t framework.TestContext) {
							// Client use new server CA cert to set up SSL connection.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.TLS, tlsContextB,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
					})
			}
		})
}

// TestSingleMTLSGateway_ServerKeyCertRotation tests a single mTLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) create two kubernetes secrets to provision server key/cert and client CA cert, and
// verify that mTLS connection could establish to deliver HTTPS request.
// (2) replace kubernetes secret to rotate server key/cert, and verify that mTLS connection could
// not establish. This is because client is still using old server CA cert to validate server cert,
// and the new server cert cannot pass validation at client side.
// (3) do another key/cert rotation to use the correct server key/cert this time, and verify that
// mTLS connection could establish to deliver HTTPS request.
func TestSingleMTLSGateway_ServerKeyCertRotation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			var (
				credName   = "testsinglemtlsgateway-serverkeycertrotation"
				credCaName = "testsinglemtlsgateway-serverkeycertrotation-cacert"
				host       = "testsinglemtlsgateway-serverkeycertrotation.example.com"
			)
			allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
			for _, instances := range allInstances {
				echotest.New(t, instances).
					SetupForDestination(func(t framework.TestContext, to echo.Target) error {
						ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
							Mode:           "MUTUAL",
							CredentialName: credName,
							Host:           host,
							ServiceName:    to.Config().Service,
							GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
						})
						return nil
					}).
					To(echotest.SingleSimplePodServiceAndAllSpecial()).
					RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
						// Add two kubernetes secrets to provision server key/cert and client CA cert for ingress gateway.
						ingressutil.CreateIngressKubeSecret(t, credCaName, ingressutil.Mtls,
							ingressutil.IngressCredentialCaCertA, false)
						ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.Mtls,
							ingressutil.IngressCredentialServerKeyCertA, false)

						ing := i.IngressFor(t.Clusters().Default())
						if ing == nil {
							t.Skip()
						}
						tlsContext := ingressutil.TLSContext{
							CaCert:     ingressutil.CaCertA,
							PrivateKey: ingressutil.TLSClientKeyA,
							Cert:       ingressutil.TLSClientCertA,
						}
						ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
							ingressutil.ExpectedResponse{StatusCode: http.StatusOK})

						t.NewSubTest("mismatched key/cert should fail").Run(func(t framework.TestContext) {
							// key/cert rotation using mis-matched server key/cert. The server cert cannot pass validation
							// at client side.
							ingressutil.RotateSecrets(t, credName, ingressutil.Mtls,
								ingressutil.IngressCredentialServerKeyCertB, false)
							// Client uses old server CA cert to set up SSL connection would fail.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{ErrorMessage: "certificate signed by unknown authority"})
						})

						t.NewSubTest("matched key/cert should succeed").Run(func(t framework.TestContext) {
							// key/cert rotation using matched server key/cert. This time the server cert is able to pass
							// validation at client side.
							ingressutil.RotateSecrets(t, credName, ingressutil.Mtls,
								ingressutil.IngressCredentialServerKeyCertA, false)
							// Use old CA cert to set up SSL connection would succeed this time.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
					})
			}
		})
}

// TestSingleOptionalMTLSGateway tests a single mTLS ingress gateway with SDS enabled.
// Verifies behavior when client sends certificate and when client does not send certificate.
func TestSingleOptionalMTLSGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			var (
				credName   = "testsinglemtlsgateway-serverkeyoptionalmtls"
				credCaName = "testsinglemtlsgateway-serverkeyoptionalmtls-cacert"
				host       = "testsinglemtlsgateway-serverkeyoptionalmtls.example.com"
			)
			allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
			for _, instances := range allInstances {
				echotest.New(t, instances).
					SetupForDestination(func(t framework.TestContext, to echo.Target) error {
						ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
							Mode:           "OPTIONAL_MUTUAL",
							CredentialName: credName,
							Host:           host,
							ServiceName:    to.Config().Service,
							GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
						})
						return nil
					}).
					To(echotest.SingleSimplePodServiceAndAllSpecial()).
					RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
						// Add two kubernetes secrets to provision server key/cert and client CA cert for ingress gateway.
						ingressutil.CreateIngressKubeSecret(t, credCaName, ingressutil.Mtls,
							ingressutil.IngressCredentialCaCertA, false)
						ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.Mtls,
							ingressutil.IngressCredentialServerKeyCertA, false)

						ing := i.IngressFor(t.Clusters().Default())
						if ing == nil {
							t.Skip()
						}
						tlsContext := ingressutil.TLSContext{
							CaCert:     ingressutil.CaCertA,
							PrivateKey: ingressutil.TLSClientKeyA,
							Cert:       ingressutil.TLSClientCertA,
						}
						t.NewSubTest("request without client certificates").Run(func(t framework.TestContext) {
							// Send a SIMPLE TLS request without client certificates.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.TLS, tlsContext,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
						t.NewSubTest("request with client certificates").Run(func(t framework.TestContext) {
							// Send a TLS request with client certificates.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
					})
			}
		})
}

// TestSingleMTLSGateway_CompoundSecretRotation tests a single mTLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) A valid kubernetes secret with key/cert and client CA cert is added, verifies that SSL connection
// termination is working properly. This secret is a compound secret.
// (2) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleMTLSGateway_CompoundSecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			var (
				credName = "testsinglemtlsgateway-generic-compoundrotation"
				host     = "testsinglemtlsgateway-compoundsecretrotation.example.com"
			)
			allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
			for _, instances := range allInstances {
				echotest.New(t, instances).
					SetupForDestination(func(t framework.TestContext, to echo.Target) error {
						ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
							Mode:           "MUTUAL",
							CredentialName: credName,
							Host:           host,
							ServiceName:    to.Config().Service,
							GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
						})
						return nil
					}).
					To(echotest.SingleSimplePodServiceAndAllSpecial()).
					RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, to echo.Target) {
						// Add kubernetes secret to provision key/cert for ingress gateway.
						ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.Mtls,
							ingressutil.IngressCredentialA, false)

						// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
						ing := i.IngressFor(t.Clusters().Default())
						tlsContext := ingressutil.TLSContext{
							CaCert:     ingressutil.CaCertA,
							PrivateKey: ingressutil.TLSClientKeyA,
							Cert:       ingressutil.TLSClientCertA,
						}
						ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
							ingressutil.ExpectedResponse{StatusCode: http.StatusOK})

						t.NewSubTest("old server CA should fail").Run(func(t framework.TestContext) {
							// key/cert rotation
							ingressutil.RotateSecrets(t, credName, ingressutil.Mtls,
								ingressutil.IngressCredentialB, false)
							// Use old server CA cert to set up SSL connection would fail.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{ErrorMessage: "certificate signed by unknown authority"})
						})

						t.NewSubTest("new server CA should succeed").Run(func(t framework.TestContext) {
							// Use new server CA cert to set up SSL connection.
							tlsContext = ingressutil.TLSContext{
								CaCert:     ingressutil.CaCertB,
								PrivateKey: ingressutil.TLSClientKeyB,
								Cert:       ingressutil.TLSClientCertB,
							}
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
					})
			}
		})
}

// TestSingleMTLSGatewayAndNotGeneric_CompoundSecretRotation tests a single mTLS ingress gateway with SDS enabled
// and use the tls cert instead of generic cert Verifies behavior in these scenarios.
// (1) A valid kubernetes secret with key/cert and client CA cert is added, verifies that SSL connection
// termination is working properly. This secret is a compound secret.
// (2) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleMTLSGatewayAndNotGeneric_CompoundSecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			var (
				credName = "testsinglemtlsgatewayandnotgeneric-compoundsecretrotation"
				host     = "testsinglemtlsgatewayandnotgeneric-compoundsecretrotation.example.com"
			)
			allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
			for _, instances := range allInstances {
				echotest.New(t, instances).
					SetupForDestination(func(t framework.TestContext, to echo.Target) error {
						ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
							Mode:           "MUTUAL",
							CredentialName: credName,
							Host:           host,
							ServiceName:    to.Config().Service,
							GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
						})
						return nil
					}).
					To(echotest.SingleSimplePodServiceAndAllSpecial()).
					RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
						// Add kubernetes secret to provision key/cert for ingress gateway.
						ingressutil.CreateIngressKubeSecret(t, credName, ingressutil.Mtls,
							ingressutil.IngressCredentialA, true)

						// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
						ing := i.IngressFor(t.Clusters().Default())
						if ing == nil {
							t.Skip()
						}
						tlsContext := ingressutil.TLSContext{
							CaCert:     ingressutil.CaCertA,
							PrivateKey: ingressutil.TLSClientKeyA,
							Cert:       ingressutil.TLSClientCertA,
						}
						ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
							ingressutil.ExpectedResponse{StatusCode: http.StatusOK})

						t.NewSubTest("old server CA should fail").Run(func(t framework.TestContext) {
							// key/cert rotation
							ingressutil.RotateSecrets(t, credName, ingressutil.Mtls,
								ingressutil.IngressCredentialB, true)
							// Use old server CA cert to set up SSL connection would fail.
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{ErrorMessage: "certificate signed by unknown authority"})
						})

						t.NewSubTest("new server CA should succeed").Run(func(t framework.TestContext) {
							// Use new server CA cert to set up SSL connection.
							tlsContext = ingressutil.TLSContext{
								CaCert:     ingressutil.CaCertB,
								PrivateKey: ingressutil.TLSClientKeyB,
								Cert:       ingressutil.TLSClientCertB,
							}
							ingressutil.SendRequestOrFail(t, ing, host, credName, ingressutil.Mtls, tlsContext,
								ingressutil.ExpectedResponse{StatusCode: http.StatusOK})
						})
					})
			}
		})
}

// TestTlsGateways deploys multiple TLS gateways with SDS enabled, and creates kubernetes that store
// private key and server certificate for each TLS gateway. Verifies that all gateways are able to terminate
// SSL connections successfully.
func TestTlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ingressutil.RunTestMultiTLSGateways(t, i, namespace.Future(&echo1NS))
		})
}

// TestMtlsGateways deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that all gateways
// are able to terminate mTLS connections successfully.
func TestMtlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ingressutil.RunTestMultiMtlsGateways(t, i, namespace.Future(&echo1NS))
		})
}

// TestMultiTlsGateway_InvalidSecret tests a single TLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiTlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			testCase := []struct {
				name                     string
				secretName               string
				ingressGatewayCredential ingressutil.IngressCredential
				hostName                 string
				expectedResponse         ingressutil.ExpectedResponse
				callType                 ingressutil.CallType
				tlsContext               ingressutil.TLSContext
			}{
				{
					name:       "tls ingress gateway invalid private key",
					secretName: "testmultitlsgateway-invalidsecret-1",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  "invalid",
						Certificate: ingressutil.TLSServerCertA,
					},
					hostName: "testmultitlsgateway-invalidsecret1.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway invalid server cert",
					secretName: "testmultitlsgateway-invalidsecret-2",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: "invalid",
					},
					hostName: "testmultitlsgateway-invalidsecret2.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway mis-matched key and cert",
					secretName: "testmultitlsgateway-invalidsecret-3",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertB,
					},
					hostName: "testmultitlsgateway-invalidsecret3.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway no private key",
					secretName: "testmultitlsgateway-invalidsecret-4",
					ingressGatewayCredential: ingressutil.IngressCredential{
						Certificate: ingressutil.TLSServerCertA,
					},
					hostName: "testmultitlsgateway-invalidsecret4.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway no server cert",
					secretName: "testmultitlsgateway-invalidsecret-5",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
					},
					hostName: "testmultitlsgateway-invalidsecret5.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
			}

			for _, c := range testCase {
				allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
				for _, instances := range allInstances {
					echotest.New(t, instances).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
								Mode:           "SIMPLE",
								CredentialName: c.secretName,
								Host:           c.hostName,
								ServiceName:    to.Config().Service,
								GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
							})
							return nil
						}).
						To(echotest.SingleSimplePodServiceAndAllSpecial()).
						RunFromClusters(func(t framework.TestContext, _ cluster.Cluster, _ echo.Target) {
							ing := i.IngressFor(t.Clusters().Default())
							if ing == nil {
								t.Skip()
							}
							t.NewSubTest(c.name).Run(func(t framework.TestContext) {
								ingressutil.CreateIngressKubeSecret(t, c.secretName, ingressutil.TLS,
									c.ingressGatewayCredential, false)

								ingressutil.SendRequestOrFail(t, ing, c.hostName, c.secretName, c.callType, c.tlsContext,
									c.expectedResponse)
							})
						})
				}
			}
		})
}

// TestMultiMtlsGateway_InvalidSecret tests a single mTLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiMtlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			testCase := []struct {
				name                     string
				secretName               string
				ingressGatewayCredential ingressutil.IngressCredential
				hostName                 string
				expectedResponse         ingressutil.ExpectedResponse
				callType                 ingressutil.CallType
				tlsContext               ingressutil.TLSContext
			}{
				{
					name:       "mtls ingress gateway invalid CA cert",
					secretName: "testmultimtlsgateway-invalidsecret-1",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
						CaCert:      "invalid",
					},
					hostName: "testmultimtlsgateway-invalidsecret1.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
				{
					name:       "mtls ingress gateway no CA cert",
					secretName: "testmultimtlsgateway-invalidsecret-2",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
					},
					hostName: "testmultimtlsgateway-invalidsecret2.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"EOF",
						},
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
				{
					name:       "mtls ingress gateway mismatched CA cert",
					secretName: "testmultimtlsgateway-invalidsecret-3",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
						CaCert:      ingressutil.CaCertB,
					},
					hostName: "testmultimtlsgateway-invalidsecret3.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						AllowedErrorMessages: []string{
							"connection reset by peer",
							"tls: error decrypting message",
						},
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
			}

			for _, c := range testCase {
				allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
				for _, instances := range allInstances {
					echotest.New(t, instances).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
								Mode:           "MUTUAL",
								CredentialName: c.secretName,
								Host:           c.hostName,
								ServiceName:    to.Config().Service,
								GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
							})
							return nil
						}).
						To(echotest.SingleSimplePodServiceAndAllSpecial()).
						RunFromClusters(func(t framework.TestContext, src cluster.Cluster, dest echo.Target) {
							ing := i.IngressFor(t.Clusters().Default())
							if ing == nil {
								t.Skip()
							}
							t.NewSubTest(c.name).Run(func(t framework.TestContext) {
								ingressutil.CreateIngressKubeSecret(t, c.secretName, ingressutil.Mtls,
									c.ingressGatewayCredential, false)

								ingressutil.SendRequestOrFail(t, ing, c.hostName, c.secretName, c.callType, c.tlsContext,
									c.expectedResponse)
							})
						})
				}
			}
		})
}

// TestMtlsGateway_CRL tests the behavior of a single mTLS ingress gateway with SDS enabled. Creates kubernetes secret
// with CRL enabled and verify the behavior.
func TestMtlsGateway_CRL(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			testCase := []struct {
				name                     string
				secretName               string
				ingressGatewayCredential ingressutil.IngressCredential
				hostName                 string
				expectedResponse         ingressutil.ExpectedResponse
				callType                 ingressutil.CallType
				tlsContext               ingressutil.TLSContext
			}{
				{
					// TC1: regular communication without CRL from client A works
					name:       "mtls ingress gateway without CRL-client A",
					secretName: "testmtlsgateway-secret-without-crl-a",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
						CaCert:      ingressutil.CaCertA,
					},
					hostName: "testmtlsgateway-crl.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode: http.StatusOK,
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
				// TC2: regular communication without CRL from client B works
				{
					name:       "mtls ingress gateway without CRL-client B",
					secretName: "testmtlsgateway-secret-without-crl-b",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyB,
						Certificate: ingressutil.TLSServerCertB,
						CaCert:      ingressutil.CaCertB,
					},
					hostName: "testmtlsgateway-crl.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode: http.StatusOK,
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertB,
						PrivateKey: ingressutil.TLSClientKeyB,
						Cert:       ingressutil.TLSClientCertB,
					},
				},
				// TC3: Add CRL with revoked client Certificate A to the configuration,
				// and initiate communication from client A.
				// Server should respond with the "revoked certificate" error message.
				{
					name:       "mtls ingress gateway with CRL-client A",
					secretName: "testmtlsgateway-secret-with-crl-a",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
						CaCert:      ingressutil.CaCertA,
						Crl:         ingressutil.CaCrlA,
					},
					hostName: "testmtlsgateway-crl.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode:   http.StatusBadGateway,
						ErrorMessage: "tls: revoked certificate",
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
				// TC4: Add CRL with a revoked dummy client certificate to the configuration,
				// and initiate communication from client A. The communication should go through
				// as long as the CRL does not have client A certificate as revoked.
				{
					name:       "mtls ingress gateway with dummy CRL",
					secretName: "testmtlsgateway-secret-with-dummy-crl",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyA,
						Certificate: ingressutil.TLSServerCertA,
						CaCert:      ingressutil.CaCertA,
						Crl:         ingressutil.DummyCaCrlA,
					},
					hostName: "testmtlsgateway-crl.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode: http.StatusOK,
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
				// TC5: Add CRL with revoked client Certificate A to the configuration,
				// and initiate communication from client B.
				// Server should respond with the "unknown CA" error message.
				{
					name:       "mtls ingress gateway with CRL-unknown CA",
					secretName: "testmtlsgateway-secret-with-crl-unknown-ca",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey:  ingressutil.TLSServerKeyB,
						Certificate: ingressutil.TLSServerCertB,
						CaCert:      ingressutil.CaCertB,
						Crl:         ingressutil.CaCrlA,
					},
					hostName: "testmtlsgateway-crl.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode:   http.StatusBadGateway,
						ErrorMessage: "tls: unknown certificate authority",
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertB,
						PrivateKey: ingressutil.TLSClientKeyB,
						Cert:       ingressutil.TLSClientCertB,
					},
				},
			}

			for _, c := range testCase {
				allInstances := []echo.Instances{ingressutil.A, ingressutil.VM}
				for _, instances := range allInstances {
					echotest.New(t, instances).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							ingressutil.SetupConfig(t, echo1NS, ingressutil.TestConfig{
								Mode:           "MUTUAL",
								CredentialName: c.secretName,
								Host:           c.hostName,
								ServiceName:    to.Config().Service,
								GatewayLabel:   i.Settings().IngressGatewayIstioLabel,
							})
							return nil
						}).
						To(echotest.SingleSimplePodServiceAndAllSpecial()).
						RunFromClusters(func(t framework.TestContext, src cluster.Cluster, dest echo.Target) {
							ing := i.IngressFor(t.Clusters().Default())
							if ing == nil {
								t.Skip()
							}
							t.NewSubTest(c.name).Run(func(t framework.TestContext) {
								ingressutil.CreateIngressKubeSecret(t, c.secretName, ingressutil.Mtls,
									c.ingressGatewayCredential, false)

								ingressutil.SendRequestOrFail(t, ing, c.hostName, c.secretName, c.callType, c.tlsContext,
									c.expectedResponse)
							})
						})
				}
			}
		})
}
