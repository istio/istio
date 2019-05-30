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
	hosts = []string{"bookinfo1.example.com", "bookinfo2.example.com", "bookinfo3.example.com",
		"bookinfo4.example.com", "bookinfo5.example.com"}
)

// TestMultiTlsGateway_InvalidSecret tests a single TLS ingress gateway with SDS enabled. Creates kubernetes secret
// with invalid key/cert and verify the behavior.
func TestMultiTlsGateway_InvalidSecret(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// TODO(JimmyCYJ): Add support into ingress package to test TLS/mTLS ingress gateway in Minikube
			//  environment
			if ctx.Environment().(*kube.Environment).Settings().Minikube {
				t.Skip("https://github.com/istio/istio/issues/14180")
			}

			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.MultiTLSGateway)

			// (1) Add invalid kubernetes secret with invalid private key.
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[0]}, ingress.TLS,
				ingressutil.IngressCredential{PrivateKey: "invalid", ServerCert: ingressutil.TLSServerCertA})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.TLS, CaCert: ingressutil.CaCertA})
			err := ingressutil.VisitProductPage(ingA, hosts[0], 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[0], err)
			}

			// (2) Add invalid kubernetes secret which has invalid server certificate.
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[1]}, ingress.TLS,
				ingressutil.IngressCredential{PrivateKey: ingressutil.TLSServerKeyA, ServerCert: "invalid"})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingB := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.TLS, CaCert: ingressutil.CaCertA})
			err = ingressutil.VisitProductPage(ingB, hosts[1], 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[1], err)
			}

			// (3) Add invalid kubernetes secret which has mis-matched private key and server certificate.
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[2]}, ingress.TLS,
				ingressutil.IngressCredential{PrivateKey: ingressutil.TLSServerKeyA, ServerCert: ingressutil.TLSServerCertB})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingC := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.TLS, CaCert: ingressutil.CaCertA})
			err = ingressutil.VisitProductPage(ingC, hosts[2], 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[2], err)
			}

			// (4) Add invalid kubernetes secret without private key.
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[3]}, ingress.TLS,
				ingressutil.IngressCredential{ServerCert: ingressutil.TLSServerCertA})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingD := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.TLS, CaCert: ingressutil.CaCertA})
			err = ingressutil.VisitProductPage(ingD, hosts[3], 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[3], err)
			}

			// (5) Add invalid kubernetes secret without server cert.
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credNames[4]}, ingress.TLS,
				ingressutil.IngressCredential{PrivateKey: ingressutil.TLSServerKeyA})
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingE := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst, IngressType: ingress.TLS, CaCert: ingressutil.CaCertA})
			err = ingressutil.VisitProductPage(ingE, hosts[4], 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", hosts[4], err)
			}
		})
}
