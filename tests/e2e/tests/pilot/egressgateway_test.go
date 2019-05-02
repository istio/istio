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
	"strings"
	"testing"

	"istio.io/istio/pkg/log"
)

// To route all external traffic via Istio Egress gateway
// 1. Add service entries
// 2. Add egress gateway
// 3. Add virtual service for each service entry such that
//    3.a. Traffic from all sidecars (i.e. mesh gateway) goes to egress gateway svc
//    3.b. Traffic from egress gateway goes to actual destination (in our case, its t)
// The tests will only check for requests from a->t with host matching ext service
func TestRouteHTTPViaEgressGateway(t *testing.T) {
	// In authn enable test, mTLS is enabled globally, which mean all clients will use TLS
	// to talk to egress-gateway. We need to explicitly specify the TLSMode to DISABLE in the
	// DestinationRule to the gateway.
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/networking/v1alpha3/disable-mtls-egressgateway.yaml",
			"testdata/networking/v1alpha3/egressgateway.yaml",
			"testdata/networking/v1alpha3/service-entry-bookinfo.yaml",
			"testdata/networking/v1alpha3/rule-route-via-egressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, "RouteHTTPViaEgressGateway", defaultRetryBudget, func() error {
			// We use an arbitrary IP to ensure that the test fails if networking logic is implemented incorrectly
			reqURL := fmt.Sprintf("http://1.1.1.1/bookinfo")
			resp := ClientRequest(cluster, "a", reqURL, 100, "--key Host --val scooby.eu.bookinfo.com")
			count := make(map[string]int)
			for _, elt := range resp.Host {
				count[elt]++
			}
			for _, elt := range resp.Code {
				count[elt]++
			}
			handledByEgress := strings.Count(resp.Body, "Handled-By-Egress-Gateway=true")
			log.Infof("request counts %v", count)
			if count["scooby.eu.bookinfo.com"] >= 95 && count[httpOK] >= 95 && handledByEgress >= 95 {
				return nil
			}
			return errAgain
		})
	}
}

// To route all external traffic to https://www.google.com via Istio Egress gateway
// 1. Add service entries
// 2. Add egress gateway
// 3. Add virtual service for each service entry such that
//    3.a. TLS Traffic from all sidecars (i.e. mesh gateway) goes to egress gateway svc
//    3.b. TLS Traffic from egress gateway goes to actual destination (in our case, www.google.com or www.bing.com)
func TestRouteSNIViaEgressGateway(t *testing.T) {
	// In authn enable test, mTLS is enabled globally, which mean all clients will use TLS
	// to talk to egress-gateway. We need to explicitly specify the TLSMode to DISABLE in the
	// DestinationRule to the gateway.
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/networking/v1alpha3/disable-mtls-egressgateway.yaml",
			"testdata/networking/v1alpha3/egressgateway.yaml",
			"testdata/networking/v1alpha3/service-entry-google.yaml",
			"testdata/networking/v1alpha3/service-entry-bing.yaml",
			"testdata/networking/v1alpha3/rule-route-via-egressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		for _, url := range []string{"https://www.google.com", "https://www.bing.com"} {
			runRetriableTest(t, "RouteSNIViaEgressGateway", defaultRetryBudget, func() error {
				reqURL := url
				resp := ClientRequest(cluster, "a", reqURL, 100, "")
				count := make(map[string]int)
				for _, elt := range resp.Code {
					count[elt]++
				}
				log.Infof("request counts %v", count)
				if count[httpOK] >= 95 {
					return nil
				}
				return errAgain
			})
		}
	}
}
