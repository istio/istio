// +build integ
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

package sdsingress

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst istio.Instance
	ns   namespace.Instance
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS Gateway flow.
	framework.
		NewSuite(m).
		Setup(istio.Setup(&inst, nil)).
		Setup(func(ctx resource.Context) (err error) {
			ns, err = ingressutil.SetupTest(ctx)
			return
		}).
		Run()
}

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
		Features("security.ingress.tls.secretrotation").
		Run(func(ctx framework.TestContext) {
			var (
				credName = "testsingletlsgateway-secretrotation"
				host     = "testsingletlsgateway-secretrotation.example.com"
			)
			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(ctx, []string{credName}, ingressutil.TLS,
				ingressutil.IngressCredentialA, false)
			defer ingressutil.DeleteKubeSecret(ctx, []string{credName})

			ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
				Mode:           "SIMPLE",
				CredentialName: credName,
				Host:           host,
			})
			ing := inst.IngressFor(ctx.Clusters().Default())

			tlsContextA := ingressutil.TLSContext{CaCert: ingressutil.CaCertA}
			tlsContextB := ingressutil.TLSContext{CaCert: ingressutil.CaCertB}

			// Verify the call works
			ingressutil.SendRequestOrFail(ctx, ing, host, credName, ingressutil.TLS, tlsContextA,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})

			// Now rotate the key/cert
			ingressutil.RotateSecrets(ctx, []string{credName}, ingressutil.TLS,
				ingressutil.IngressCredentialB, false)

			ctx.NewSubTest("old cert should fail").Run(func(ctx framework.TestContext) {
				// Client use old server CA cert to set up SSL connection would fail.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName, ingressutil.TLS, tlsContextA,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"})
			})

			ctx.NewSubTest("new cert should succeed").Run(func(ctx framework.TestContext) {
				// Client use new server CA cert to set up SSL connection.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName, ingressutil.TLS, tlsContextB,
					ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})
			})
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
		Features("security.ingress.mtls.secretrotation").
		Run(func(ctx framework.TestContext) {
			var (
				credName   = []string{"testsinglemtlsgateway-serverkeycertrotation"}
				credCaName = []string{"testsinglemtlsgateway-serverkeycertrotation-cacert"}
				host       = "testsinglemtlsgateway-serverkeycertrotation.example.com"
			)

			ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
				Mode:           "MUTUAL",
				CredentialName: credName[0],
				Host:           host,
			})

			// Add two kubernetes secrets to provision server key/cert and client CA cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(ctx, credCaName, ingressutil.Mtls,
				ingressutil.IngressCredentialCaCertA, false)
			ingressutil.CreateIngressKubeSecret(ctx, credName, ingressutil.Mtls,
				ingressutil.IngressCredentialServerKeyCertA, false)
			defer ingressutil.DeleteKubeSecret(ctx, credName)
			defer ingressutil.DeleteKubeSecret(ctx, credCaName)

			ing := inst.IngressFor(ctx.Clusters().Default())
			tlsContext := ingressutil.TLSContext{
				CaCert:     ingressutil.CaCertA,
				PrivateKey: ingressutil.TLSClientKeyA,
				Cert:       ingressutil.TLSClientCertA,
			}
			ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})

			ctx.NewSubTest("mismatched key/cert should fail").Run(func(ctx framework.TestContext) {
				// key/cert rotation using mis-matched server key/cert. The server cert cannot pass validation
				// at client side.
				ingressutil.RotateSecrets(ctx, credName, ingressutil.Mtls,
					ingressutil.IngressCredentialServerKeyCertB, false)
				// Client uses old server CA cert to set up SSL connection would fail.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"})
			})

			ctx.NewSubTest("matched key/cert should succeed").Run(func(ctx framework.TestContext) {
				// key/cert rotation using matched server key/cert. This time the server cert is able to pass
				// validation at client side.
				ingressutil.RotateSecrets(ctx, credName, ingressutil.Mtls,
					ingressutil.IngressCredentialServerKeyCertA, false)
				// Use old CA cert to set up SSL connection would succeed this time.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})
			})
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
		Features("security.ingress.mtls.generic-compoundrotation").
		Run(func(ctx framework.TestContext) {
			var (
				credName = []string{"testsinglemtlsgateway-generic-compoundrotation"}
				host     = "testsinglemtlsgateway-compoundsecretrotation.example.com"
			)

			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(ctx, credName, ingressutil.Mtls,
				ingressutil.IngressCredentialA, false)
			defer ingressutil.DeleteKubeSecret(ctx, credName)

			ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
				Mode:           "MUTUAL",
				CredentialName: credName[0],
				Host:           host,
			})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			ing := inst.IngressFor(ctx.Clusters().Default())
			tlsContext := ingressutil.TLSContext{
				CaCert:     ingressutil.CaCertA,
				PrivateKey: ingressutil.TLSClientKeyA,
				Cert:       ingressutil.TLSClientCertA,
			}
			ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})

			ctx.NewSubTest("old server CA should fail").Run(func(ctx framework.TestContext) {
				// key/cert rotation
				ingressutil.RotateSecrets(ctx, credName, ingressutil.Mtls,
					ingressutil.IngressCredentialB, false)
				// Use old server CA cert to set up SSL connection would fail.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"})
			})

			ctx.NewSubTest("new server CA should succeed").Run(func(ctx framework.TestContext) {
				// Use new server CA cert to set up SSL connection.
				tlsContext = ingressutil.TLSContext{
					CaCert:     ingressutil.CaCertB,
					PrivateKey: ingressutil.TLSClientKeyB,
					Cert:       ingressutil.TLSClientCertB,
				}
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})
			})
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
		Features("security.ingress.mtls.nongeneric-compoundrotation").
		Run(func(ctx framework.TestContext) {
			var (
				credName = []string{"testsinglemtlsgatewayandnotgeneric-compoundsecretrotation"}
				host     = "testsinglemtlsgatewayandnotgeneric-compoundsecretrotation.example.com"
			)

			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(ctx, credName, ingressutil.Mtls,
				ingressutil.IngressCredentialA, true)
			defer ingressutil.DeleteKubeSecret(ctx, credName)

			ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
				Mode:           "MUTUAL",
				CredentialName: credName[0],
				Host:           host,
			})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			ing := inst.IngressFor(ctx.Clusters().Default())
			tlsContext := ingressutil.TLSContext{
				CaCert:     ingressutil.CaCertA,
				PrivateKey: ingressutil.TLSClientKeyA,
				Cert:       ingressutil.TLSClientCertA,
			}
			ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})

			ctx.NewSubTest("old server CA should fail").Run(func(ctx framework.TestContext) {
				// key/cert rotation
				ingressutil.RotateSecrets(ctx, credName, ingressutil.Mtls,
					ingressutil.IngressCredentialB, true)
				// Use old server CA cert to set up SSL connection would fail.
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"})
			})

			ctx.NewSubTest("new server CA should succeed").Run(func(ctx framework.TestContext) {
				// Use new server CA cert to set up SSL connection.
				tlsContext = ingressutil.TLSContext{
					CaCert:     ingressutil.CaCertB,
					PrivateKey: ingressutil.TLSClientKeyB,
					Cert:       ingressutil.TLSClientCertB,
				}
				ingressutil.SendRequestOrFail(ctx, ing, host, credName[0], ingressutil.Mtls, tlsContext,
					ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""})
			})
		})
}

// TestTlsGateways deploys multiple TLS gateways with SDS enabled, and creates kubernetes that store
// private key and server certificate for each TLS gateway. Verifies that all gateways are able to terminate
// SSL connections successfully.
func TestTlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.tls.gateway.valid-secret").
		Run(func(ctx framework.TestContext) {
			ingressutil.RunTestMultiTLSGateways(ctx, inst, ns)
		})
}

// TestMtlsGateways deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that all gateways
// are able to terminate mTLS connections successfully.
func TestMtlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.mtls.gateway").
		Run(func(ctx framework.TestContext) {
			ingressutil.RunTestMultiMtlsGateways(ctx, inst, ns)
		})
}

// TestMultiTlsGateway_InvalidSecret tests a single TLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiTlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.tls.gateway.invalid-secret").
		Run(func(ctx framework.TestContext) {
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
						PrivateKey: "invalid",
						ServerCert: ingressutil.TLSServerCertA,
					},
					hostName: "testmultitlsgateway-invalidsecret1.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						// TODO(JimmyCYJ): Temporarily skip verification of error message to deflake test.
						//  Need a more accurate way to verify the request failures.
						// https://github.com/istio/istio/issues/16998
						ErrorMessage: "",
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
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: "invalid",
					},
					hostName: "testmultitlsgateway-invalidsecret2.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
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
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertB,
					},
					hostName: "testmultitlsgateway-invalidsecret3.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
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
						ServerCert: ingressutil.TLSServerCertA,
					},
					hostName: "testmultitlsgateway-invalidsecret4.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
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
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingressutil.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
			}

			ing := inst.IngressFor(ctx.Clusters().Default())
			for _, c := range testCase {
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					ingressutil.CreateIngressKubeSecret(ctx, []string{c.secretName}, ingressutil.TLS,
						c.ingressGatewayCredential, false)
					defer ingressutil.DeleteKubeSecret(ctx, []string{c.secretName})
					ctx.Cleanup(ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
						Mode:           "SIMPLE",
						CredentialName: c.secretName,
						Host:           c.hostName,
					}))
					ingressutil.SendRequestOrFail(ctx, ing, c.hostName, c.secretName, c.callType, c.tlsContext,
						c.expectedResponse)
				})
			}
		})
}

// TestMultiMtlsGateway_InvalidSecret tests a single mTLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiMtlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.mtls.gateway").
		Run(func(ctx framework.TestContext) {
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
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertA,
						CaCert:     "invalid",
					},
					hostName: "testmultimtlsgateway-invalidsecret1.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						// TODO(JimmyCYJ): Temporarily skip verification of error message to deflake test.
						//  Need a more accurate way to verify the request failures.
						// https://github.com/istio/istio/issues/16998
						ErrorMessage: "",
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
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertA,
					},
					hostName: "testmultimtlsgateway-invalidsecret2.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
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
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertA,
						CaCert:     ingressutil.CaCertB,
					},
					hostName: "testmultimtlsgateway-invalidsecret3.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingressutil.Mtls,
					tlsContext: ingressutil.TLSContext{
						CaCert:     ingressutil.CaCertA,
						PrivateKey: ingressutil.TLSClientKeyA,
						Cert:       ingressutil.TLSClientCertA,
					},
				},
			}

			ing := inst.IngressFor(ctx.Clusters().Default())
			for _, c := range testCase {
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					ingressutil.CreateIngressKubeSecret(ctx, []string{c.secretName}, ingressutil.Mtls,
						c.ingressGatewayCredential, false)
					defer ingressutil.DeleteKubeSecret(ctx, []string{c.secretName})

					ingressutil.SetupConfig(ctx, ns, ingressutil.TestConfig{
						Mode:           "MUTUAL",
						CredentialName: c.secretName,
						Host:           c.hostName,
					})
					ingressutil.SendRequestOrFail(ctx, ing, c.hostName, c.secretName, c.callType, c.tlsContext,
						c.expectedResponse)
				})
			}
		})
}
