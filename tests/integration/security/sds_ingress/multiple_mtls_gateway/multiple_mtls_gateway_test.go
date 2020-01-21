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

package multiplemtlsgateway

import (
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
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

// testMultiMtlsGateways deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that all gateways are able to terminate
// mTLS connections successfully.
func testMultiMtlsGateways(t *testing.T, ctx framework.TestContext) { // nolint:interfacer
	t.Helper()

	ingressutil.CreateIngressKubeSecret(t, ctx, credNames, ingress.Mtls, ingressutil.IngressCredentialA)
	ingressutil.DeployBookinfo(t, ctx, g, ingressutil.MultiMTLSGateway)

	ing := ingress.NewOrFail(t, ctx, ingress.Config{
		Istio: inst,
	})
	// Expect 2 SDS updates for each listener, one for server key/cert, and one for CA cert.
	err := ingressutil.WaitUntilGatewaySdsStatsGE(t, ing, 2*len(credNames), 30*time.Second)
	if err != nil {
		t.Errorf("sds update stats does not match: %v", err)
	}
	// Expect 2 active listeners, one listens on 443 and the other listens on 15090
	err = ingressutil.WaitUntilGatewayActiveListenerStatsGE(t, ing, 2, 60*time.Second)
	if err != nil {
		t.Errorf("total active listener stats does not match: %v", err)
	}
	tlsContext := ingressutil.TLSContext{
		CaCert:     ingressutil.CaCertA,
		PrivateKey: ingressutil.TLSClientKeyA,
		Cert:       ingressutil.TLSClientCertA,
	}
	callType := ingress.Mtls

	for _, h := range hosts {
		err := ingressutil.VisitProductPage(ing, h, callType, tlsContext, 30*time.Second,
			ingressutil.ExpectedResponse{ResponseCode: 200, ErrorMessage: ""}, t)
		if err != nil {
			t.Errorf("unable to retrieve 200 from product page at host %s: %v", h, err)
		}
	}
}

func TestMtlsGateways(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			testMultiMtlsGateways(t, ctx)
		})
}
