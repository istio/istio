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
	if !tc.V1alpha3 {
		t.Skipf("Skipping %s: v1alpha3=false", t.Name())
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

// TODO: rename this file gateway_test.go, merge w/ egress too? At least this test and test above
// use gateway as an "ingress" of sorts.
func TestGateway_TCP(t *testing.T) {
	if !tc.V1alpha3 {
		t.Skipf("Skipping %s: V1alpha3=false", t.Name())
	}
	// TODO: use current namespace so test doesn't require --cluster_wide flag
	// circle CI always runs with --cluster_wide, and its required for gateway tests atm due to
	// gateway resource only being created in istio-system namespace
	istioNamespace := tc.Kube.IstioSystemNamespace()

	cfgs := &deployableConfig{
		Namespace: istioNamespace,
		YamlFiles: []string{
			"testdata/v1alpha3/rule-force-a-through-ingress-gateway.yaml",
			"testdata/v1alpha3/rule-gateway-a.yaml",
			"testdata/v1alpha3/gateway-tcp-a.yaml",
		},
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	cases := []struct {
		// empty destination to expect 404
		dst string
		url string
	}{
		{
			dst: "a",
			url: fmt.Sprintf("http://%s.%s:%d", "a", istioNamespace, 9090),
		},
	}
	t.Run("tcp_requests", func(t *testing.T) {
		for _, c := range cases {
			runRetriableTest(t, c.url, defaultRetryBudget, func() error {
				resp := ClientRequest("b", c.url, 1, "")
				if resp.IsHTTPOk() {
					return nil
				}
				return errAgain
			})
		}
	})
}
