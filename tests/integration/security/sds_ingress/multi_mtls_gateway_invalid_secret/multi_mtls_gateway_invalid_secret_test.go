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

package multimtlsgatewayinvalidsecret

import (
"time"

"istio.io/istio/pkg/test/framework"
"istio.io/istio/pkg/test/framework/components/environment"
"istio.io/istio/pkg/test/framework/components/environment/kube"
"istio.io/istio/pkg/test/framework/components/ingress"
ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"

"testing"
)

var (
	credNames = []string{"bookinfo-credential-1", "bookinfo-credential-2", "bookinfo-credential-3",
		"bookinfo-credential-4", "bookinfo-credential-5"}
	hosts     = []string{"bookinfo1.example.com", "bookinfo2.example.com", "bookinfo3.example.com",
		"bookinfo4.example.com", "bookinfo5.example.com"}
)

// TestMultiMtlsGateway_InvalidSecret tests a single mTLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiMtlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
			Run(func(ctx framework.TestContext) {
				// TODO(JimmyCYJ): Add support into ingress package to test TLS/mTLS ingress gateway in Minikube
				//  environment
				if ctx.Environment().(*kube.Environment).Settings().Minikube {
					t.Skip("https://github.com/istio/istio/issues/14180")
				}

				ingressutil.DeployBookinfo(t, ctx, g, ingressutil.MultiMTLSGateway)

				// (1) Add invalid kubernetes secret with invalid CA cert.
				ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[0]}, ingress.Mtls,
					ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertA,
						CaCert:			"invalid",
					})
				// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
				time.Sleep(3 * time.Second)
				ingA := ingress.NewOrFail(t, ctx, ingress.Config{
					Istio:       inst,
					IngressType: ingress.Mtls,
					CaCert:      ingressutil.CaCertA,
					PrivateKey:  ingressutil.TLSClientKeyA,
					Cert:        ingressutil.TLSClientCertA,
				})
				err := ingressutil.VisitProductPage(ingA, hosts[0], 30*time.Second,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
				if err != nil {
					t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[0], err)
				}

				// (2) Add invalid kubernetes secret which does not have CA certificate.
				ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[1]}, ingress.Mtls,
					ingressutil.IngressCredential{PrivateKey: ingressutil.TLSServerKeyA, ServerCert: ingressutil.TLSServerCertA})
				// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
				time.Sleep(3 * time.Second)
				ingB := ingress.NewOrFail(t, ctx, ingress.Config{
					Istio:       inst,
					IngressType: ingress.Mtls,
					CaCert:      ingressutil.CaCertA,
					PrivateKey:  ingressutil.TLSClientKeyA,
					Cert:        ingressutil.TLSClientCertA,
				})
				err = ingressutil.VisitProductPage(ingB, hosts[1], 30*time.Second,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
				if err != nil {
					t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[1], err)
				}

				// (3) Add invalid kubernetes secret which has a CA certificate that could not verify client certificate
				ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[2]}, ingress.Mtls,
					ingressutil.IngressCredential{
						PrivateKey: ingressutil.TLSServerKeyA,
						ServerCert: ingressutil.TLSServerCertA,
						CaCert: 		ingressutil.CaCertB,
					})
				// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
				time.Sleep(3 * time.Second)
				ingC := ingress.NewOrFail(t, ctx, ingress.Config{
					Istio:       inst,
					IngressType: ingress.Mtls,
					CaCert:      ingressutil.CaCertA,
					PrivateKey:  ingressutil.TLSClientKeyA,
					Cert:        ingressutil.TLSClientCertA,
				})
				err = ingressutil.VisitProductPage(ingC, hosts[2], 30*time.Second,
					ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
				if err != nil {
					t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[2], err)
				}
			})
}



