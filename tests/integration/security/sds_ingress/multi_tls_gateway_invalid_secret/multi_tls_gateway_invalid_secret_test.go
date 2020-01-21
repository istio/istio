// Copyright 2019 Istio Authors
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

package multitlsgatewayinvalidsecret

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/ingress"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// TestMultiTlsGateway_InvalidSecret tests a single TLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiTlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.MultiTLSGateway)

			testCase := []struct {
				name                     string
				secretName               string
				ingressGatewayCredential ingressutil.IngressCredential
				ingressConfig            ingress.Config
				hostName                 string
				expectedResponse         ingressutil.ExpectedResponse
				callType                 ingress.CallType
				tlsContext               ingressutil.TLSContext
			}{
				{
					name:       "tls ingress gateway invalid private key",
					secretName: "bookinfo-credential-1",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey: "invalid",
						ServerCert: ingressutil.TLSServerCertA,
					},
					ingressConfig: ingress.Config{
						Istio: inst,
					},
					hostName: "bookinfo1.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						// TODO(JimmyCYJ): Temporarily skip verification of error message to deflake test.
						//  Need a more accurate way to verify the request failures.
						// https://github.com/istio/istio/issues/16998
						ErrorMessage: "",
					},
					callType: ingress.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway invalid server cert",
					secretName: "bookinfo-credential-2",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: "invalid",
					},
					ingressConfig: ingress.Config{
						Istio: inst,
					},
					hostName: "bookinfo2.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingress.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway mis-matched key and cert",
					secretName: "bookinfo-credential-3",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertB,
					},
					ingressConfig: ingress.Config{
						Istio: inst,
					},
					hostName: "bookinfo3.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingress.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway no private key",
					secretName: "bookinfo-credential-4",
					ingressGatewayCredential: ingressutil.IngressCredential{
						ServerCert: ingressutil.TLSServerCertA,
					},
					ingressConfig: ingress.Config{
						Istio: inst,
					},
					hostName: "bookinfo4.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingress.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
				{
					name:       "tls ingress gateway no server cert",
					secretName: "bookinfo-credential-5",
					ingressGatewayCredential: ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
					},
					ingressConfig: ingress.Config{
						Istio: inst,
					},
					hostName: "bookinfo5.example.com",
					expectedResponse: ingressutil.ExpectedResponse{
						ResponseCode: 0,
						ErrorMessage: "",
					},
					callType: ingress.TLS,
					tlsContext: ingressutil.TLSContext{
						CaCert: ingressutil.CaCertA,
					},
				},
			}

			for _, c := range testCase {
				ingressutil.CreateIngressKubeSecret(t, ctx, []string{c.secretName}, ingress.TLS,
					c.ingressGatewayCredential)
				// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
				time.Sleep(3 * time.Second)
				ing := ingress.NewOrFail(t, ctx, c.ingressConfig)
				err := ingressutil.VisitProductPage(ing, c.hostName, c.callType, c.tlsContext, 30*time.Second, c.expectedResponse, t)
				if err != nil {
					t.Errorf("test case %s: unable to retrieve %d from product page at host %s: %v",
						c.name, c.expectedResponse.ResponseCode, c.hostName, err)
				}
			}
		})
}
