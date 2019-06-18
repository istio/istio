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

package singletlsgateway

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
	credName = []string{"bookinfo-credential-1"}
	host     = "bookinfo1.example.com"
)

// TestSingleTlsGateway_SecretRotation tests a single TLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) when kubernetes secret is not provisioned, which means private key and server certificate
// are not available. Verifies that listener is not up and connection creation get rejected.
// (2) A valid kubernetes secret with key/cert is added later, verifies that SSL connection termination is working properly.
// (3) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleTlsGateway_SecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// TODO(JimmyCYJ): Add support into ingress package to test TLS/mTLS ingress gateway in Minikube
			//  environment https://github.com/istio/istio/issues/14180.
			if ctx.Environment().(*kube.Environment).Settings().Minikube {
				t.Skip("https://github.com/istio/istio/issues/14180")
			}

			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.SingleTLSGateway)

			// Do not provide private key and server certificate for ingress gateway. Connection creation should fail.
			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			tlsContext := ingressutil.TLSContext{CaCert: ingressutil.CaCertA}
			err := ingressutil.VisitProductPage(ingA, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "connection refused"}, t)
			if err != nil {
				t.Errorf("unable to retrieve code 0 from product page at host %s: %v", host, err)
			}

			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(t, ctx, credName, ingress.TLS, ingressutil.IngressCredentialA)
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)
			ingB := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			err = ingressutil.VisitProductPage(ingB, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}

			// key/cert rotation
			ingressutil.RotateSecrets(t, ctx, credName, ingress.TLS, ingressutil.IngressCredentialB)
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			time.Sleep(3 * time.Second)

			// Use old CA cert to set up SSL connection would fail.
			err = ingressutil.VisitProductPage(ingB, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 404 from product page at host %s: %v", host, err)
			}

			// Use new CA cert to set up SSL connection.
			tlsContext = ingressutil.TLSContext{CaCert: ingressutil.CaCertB}
			ingC := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			err = ingressutil.VisitProductPage(ingC, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}
		})
}
