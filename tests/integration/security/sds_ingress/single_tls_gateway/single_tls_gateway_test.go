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

// TestSingleTlsGateway_SecretRotation tests a single TLS ingress gateway with SDS enabled.
// Verifies behavior in these scenarios.
// (1) A valid kubernetes secret with key/cert is added, verifies that SSL connection termination
// is working properly.
// (2) After key/cert rotation, client needs to pick new CA cert to complete SSL connection. Old CA
// cert will cause the SSL connection fail.
func TestSingleTlsGateway_SecretRotation(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			// Add kubernetes secret to provision key/cert for ingress gateway.
			ingressutil.CreateIngressKubeSecret(t, ctx, credName, ingress.TLS, ingressutil.IngressCredentialA)
			ingressutil.DeployBookinfo(t, ctx, g, ingressutil.SingleTLSGateway)

			ingA := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			err := ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 1, 10*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}
			// Expect 2 active listeners, one listens on 443 and the other listens on 15090
			err = ingressutil.WaitUntilGatewayActiveListenerStatsGE(t, ingA, 2, 60*time.Second)
			if err != nil {
				t.Errorf("total active listener stats does not match: %v", err)
			}
			tlsContext := ingressutil.TLSContext{CaCert: ingressutil.CaCertA}
			err = ingressutil.VisitProductPage(ingA, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}

			// key/cert rotation
			ingressutil.RotateSecrets(t, ctx, credName, ingress.TLS, ingressutil.IngressCredentialB)
			err = ingressutil.WaitUntilGatewaySdsStatsGE(t, ingA, 2, 10*time.Second)
			if err != nil {
				t.Errorf("sds update stats does not match: %v", err)
			}

			// Client use old server CA cert to set up SSL connection would fail.
			err = ingressutil.VisitProductPage(ingA, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 0, ErrorMessage: "certificate signed by unknown authority"}, t)
			if err != nil {
				t.Errorf("unable to retrieve 404 from product page at host %s: %v", host, err)
			}

			// Client use new server CA cert to set up SSL connection.
			tlsContext = ingressutil.TLSContext{CaCert: ingressutil.CaCertB}
			ingB := ingress.NewOrFail(t, ctx, ingress.Config{Istio: inst})
			err = ingressutil.VisitProductPage(ingB, host, ingress.TLS, tlsContext, 30*time.Second,
				ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
			if err != nil {
				t.Errorf("unable to retrieve 200 from product page at host %s: %v", host, err)
			}
		})
}
