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

package singlemtlsgatewayseparatesecret

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/ingress"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	credName   = []string{"bookinfo-credential-1"}
	credCaName = []string{"bookinfo-credential-1-cacert"}
	host       = "bookinfo1.example.com"
)

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
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.SingleMTLSGateway)

			// Add two kubernetes secrets to provision server key/cert and client CA cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(t, ctx, credCaName, ingress.Mtls, ingressutil.IngressCredentialCaCertA)
			ingressutil.CreateIngressKubeSecret(t, ctx, credName, ingress.Mtls,
				ingressutil.IngressCredentialServerKeyCertA)

			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			// Expect 2 SDS updates, one for the server key/cert update, and one for the CA cert update.
			err := ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 2, 10*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}
			// Expect 2 active listeners, one listens on 443 and the other listens on 15090
			err = ingressutil.WaitUntilGatewayActiveListenerStatsGE(t, ingA, 2, 60*time.Second)
			if err != nil {
				t.Errorf("total active listener stats does not match: %v", err)
			}
			tlsContext := ingressutil.TLSContext{
				CaCert:     ingressutil.CaCertA,
				PrivateKey: ingressutil.TLSClientKeyA,
				Cert:       ingressutil.TLSClientCertA,
			}
			err = ingressutil.VisitProductPage(ingA, host, ingress.Mtls, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve code 200 from product page at host %s: %v", host, err)
			}

			// key/cert rotation using mis-matched server key/cert. The server cert cannot pass validation
			// at client side.
			ingressutil.RotateSecrets(t, ctx, credName, ingress.Mtls, ingressutil.IngressCredentialServerKeyCertB)
			// Expect 1 more SDS updates for the server key/cert update.
			err = ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 3, 30*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}
			// Client uses old server CA cert to set up SSL connection would fail.
			err = ingressutil.VisitProductPage(ingA, host, ingress.Mtls, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 0 from product page at host %s: %v", host, err)
			}

			// key/cert rotation using matched server key/cert. This time the server cert is able to pass
			// validation at client side.
			ingressutil.RotateSecrets(t, ctx, credName, ingress.Mtls, ingressutil.IngressCredentialServerKeyCertA)
			// Expect 1 more SDS updates for the server key/cert update.
			err = ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 4, 30*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}
			// Use old CA cert to set up SSL connection would succeed this time.
			err = ingressutil.VisitProductPage(ingA, host, ingress.Mtls, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve code 200 from product page at host %s: %v", host, err)
			}
		})
}
