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
	"strconv"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

// maybeAddTLSForDestinationRule fills the DestinationRule template if the mTLS is turned on globally.
func maybeAddTLSForDestinationRule(tc *testConfig, templateFile string) string {
	outYaml, _ := util.CreateAndFill(tc.Info.TempDir, templateFile, map[string]string{
		"globalMTlsEnable": strconv.FormatBool(tc.Kube.AuthEnabled),
	})
	return outYaml
}

// The gateway is just another service.
// So we try to reach this gateway from service t (without sidecar)
// but use the hostname (uk.bookinfo.com) with which we configured the gateway so that
// we match the virtual host in the gateway.
// The backend for uk.bookinfo.com is set to c-v2 using the rule-ingressgateway.yaml
// virtual service spec.
// The gateways have already been deployed by the helm charts, and are configured by
// default (kube service level) to expose ports 80/443. So our gateway specs also expose
// ports 80/443.
func TestGateway_HTTPIngress(t *testing.T) {
	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	// Configure a route from us.bookinfo.com to "c-v2" only
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/networking/v1alpha3/ingressgateway.yaml",
			maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml"),
			"testdata/networking/v1alpha3/rule-ingressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "HTTPIngressGateway", defaultRetryBudget, func() error {
			reqURL := fmt.Sprintf("http://%s.%s/c", ingressGatewayServiceName, istioNamespace)
			resp := ClientRequest(cluster, "t", reqURL, 100, "-key Host -val uk.bookinfo.com")
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
}

func TestGateway_HTTPSIngress(t *testing.T) {
	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	// Configure a route from us.bookinfo.com to "c-v2" only
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/networking/v1alpha3/ingressgateway.yaml",
			maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml"),
			"testdata/networking/v1alpha3/rule-ingressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "HTTPSIngressGateway", defaultRetryBudget, func() error {
			reqURL := fmt.Sprintf("https://%s.%s/c", ingressGatewayServiceName, istioNamespace)
			resp := ClientRequest(cluster, "t", reqURL, 100, "-key Host -val uk.bookinfo.com")
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
}

func TestGateway_TCPIngress(t *testing.T) {
	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	// Configure a route from us.bookinfo.com to "c-v2" only
	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/networking/v1alpha3/ingressgateway.yaml",
			maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/destination-rule-c.yaml"),
			"testdata/networking/v1alpha3/rule-ingressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	for cluster := range tc.Kube.Clusters {
		runRetriableTest(t, cluster, "TCPIngressGateway", defaultRetryBudget, func() error {
			reqURL := fmt.Sprintf("http://%s.%s:31400/c", ingressGatewayServiceName, istioNamespace)
			resp := ClientRequest(cluster, "t", reqURL, 100, "-key Host -val uk.bookinfo.com")
			count := make(map[string]int)
			for _, elt := range resp.Version {
				count[elt] = count[elt] + 1
			}
			log.Infof("request counts %v", count)
			if count["v1"] >= 95 {
				return nil
			}
			return errAgain
		})
	}
}

func TestIngressGateway503DuringRuleChange(t *testing.T) {
	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	gateway := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/networking/v1alpha3/ingressgateway.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}

	// Add subsets
	newDestRule := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/rule-503test-destinationrule-c.yaml")},
		kubeconfig: tc.Kube.KubeConfig,
	}

	// route to subsets
	newVirtService := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/networking/v1alpha3/rule-503test-virtualservice.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}

	addMoreSubsets := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/rule-503test-destinationrule-c-add-subset.yaml")},
		kubeconfig: tc.Kube.KubeConfig,
	}

	routeToNewSubsets := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/networking/v1alpha3/rule-503test-update-virtualservice.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}

	deleteOldSubsets := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{maybeAddTLSForDestinationRule(tc, "testdata/networking/v1alpha3/rule-503test-destinationrule-c-del-subset.yaml")},
		kubeconfig: tc.Kube.KubeConfig,
	}

	var resp ClientResponse
	var err error
	var fatalError bool

	if err = gateway.Setup(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Teardown()

	log.Infof("Adding new subsets v1,v2")
	// these functions have built in sleep. So we don't have to add any extra sleep here
	if err = newDestRule.Setup(); err != nil {
		t.Fatal(err)
	}
	defer newDestRule.Teardown()

	log.Infof("routing to v1,v2")
	if err = newVirtService.Setup(); err != nil {
		t.Fatal(err)
	}
	defer newVirtService.Teardown()

	time.Sleep(2 * time.Second)
	var wg sync.WaitGroup
	for cluster := range tc.Kube.Clusters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reqURL := fmt.Sprintf("http://%s.%s/c", ingressGatewayServiceName, istioNamespace)
			// 500 requests @20 qps = 25s. This is the minimum required to cover all rule changes below.
			resp = ClientRequest(cluster, "t", reqURL, 500, "-key Host -val uk.bookinfo.com -qps 20")
		}()
	}

	log.Infof("Adding new subsets v3,v4")
	if err = addMoreSubsets.Setup(); err != nil {
		fatalError = true
		goto cleanup
	}
	time.Sleep(2 * time.Second)
	log.Infof("routing to v3,v4")
	if err = routeToNewSubsets.Setup(); err != nil {
		fatalError = true
		goto cleanup
	}
	time.Sleep(2 * time.Second)
	log.Infof("deleting old subsets v1,v2")
	if err = deleteOldSubsets.Setup(); err != nil {
		fatalError = true
		goto cleanup
	}

cleanup:
	wg.Wait()
	if fatalError {
		t.Fatal(err)
	} else {
		//log.Infof("Body: %s, response codes: %v", resp.Body, resp.Code)
		if len(resp.Code) > 0 {
			count := make(map[string]int)
			for _, elt := range resp.Code {
				count[elt] = count[elt] + 1
			}
			if count["200"] != len(resp.Code) {
				// have entries other than 200
				t.Errorf("Got non 200 status code while changing rules: %v", count)
			} else {
				log.Infof("No 503s were encountered while changing rules (total %d requests)", len(resp.Code))
			}
		} else {
			t.Errorf("Could not parse response codes from the client")
		}
	}
}
