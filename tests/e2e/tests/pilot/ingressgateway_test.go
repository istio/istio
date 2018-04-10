// Copyright 2017 Istio Authors
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

package pilot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/log"
)

// The gateway is just another service.
// So we try to reach this gateway from service t (without sidecar)
// but use the hostname (uk.bookinfo.com) with which we configured the gateway so that
// we match the virtual host in the gateway.
// The backend for uk.bookinfo.com is set to c-v2 using the rule-ingressgateway.yaml
// virtual service spec.
// The gateways have already been deployed by the helm charts, and are configured by
// default (kube service level) to expose ports 80/443. So our gateway specs also expose
// ports 80/443.
func TestIngressGateway(t *testing.T) {
	if !tc.V1alpha3 || tc.Kube.AuthEnabled {
		t.Skipf("Skipping %s: v1alpha3=false or auth_enable=true", t.Name())
	}

	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	// Configure a route from us.bookinfo.com to "c-v2" only
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/v1alpha3/ingressgateway.yaml",
			"testdata/v1alpha3/destination-rule-c.yaml",
			"testdata/v1alpha3/rule-ingressgateway.yaml"},
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	runRetriableTest(t, "VersionRouting", defaultRetryBudget, func() error {
		reqURL := fmt.Sprintf("http://%s.%s/c", ingressGatewayServiceName, istioNamespace)
		resp := ClientRequest("t", reqURL, 100, "-key Host -val uk.bookinfo.com")
		count := make(map[string]int)
		for _, elt := range resp.Version {
			count[elt] = count[elt] + 1
		}
		log.Infof("request counts %v", count)
		if count["v2"] >= 95 {
			return nil
		}
		return errAgain
	})
}
