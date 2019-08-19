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

package singlemtlsgatewaycompoundsecret

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/ingress"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	credName = []string{"bookinfo-credential-1"}
	host     = "bookinfo1.example.com"
)

// TestSingleMTLSGateway_CompoundSecretRotation tests a single mTLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) A valid kubernetes secret with key/cert and client CA cert is added, verifies that SSL connection
// termination is working properly. This secret is a compound secret.
// (2) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleMTLSGateway_CompoundSecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(t, ctx, credName, ingress.Mtls, ingressutil.IngressCredentialA)
			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.SingleMTLSGateway)
			// Wait for ingress gateway to fetch key/cert from Gateway agent via SDS.
			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			// Expect 2 SDS updates, one for the server key/cert update, and one for the CA cert update.
			err := ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 2, 30*time.Second)
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
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}

			// key/cert rotation
			ingressutil.RotateSecrets(t, ctx, credName, ingress.Mtls, ingressutil.IngressCredentialB)
			// Expect 2 more SDS updates, one for the server key/cert update, and one for the CA cert update.
			err = ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 4, 30*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}
			// Use old server CA cert to set up SSL connection would fail.
			err = ingressutil.VisitProductPage(ingA, host, ingress.Mtls, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 404 from product page at host %s: %v", host, err)
			}

			// Use new server CA cert to set up SSL connection.
			ingB := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			tlsContext = ingressutil.TLSContext{
				CaCert:     ingressutil.CaCertB,
				PrivateKey: ingressutil.TLSClientKeyB,
				Cert:       ingressutil.TLSClientCertB,
			}
			err = ingressutil.VisitProductPage(ingB, host, ingress.Mtls, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}
		})
}
