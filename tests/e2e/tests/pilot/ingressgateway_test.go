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
	"time"

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

func TestIngressGateway503DuringRuleChange(t *testing.T) {
	if !tc.V1alpha3 {
		t.Skipf("Skipping %s: v1alpha3=false", t.Name())
	}

	istioNamespace := tc.Kube.IstioSystemNamespace()
	ingressGatewayServiceName := tc.Kube.IstioIngressGatewayService()

	gateway := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/ingressgateway.yaml"},
	}

	// Add subsets
	newDestRule := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/rule-503test-destinationrule-c.yaml"},
	}

	// route to subsets
	newVirtService := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/rule-503test-virtualservice.yaml"},
	}

	addMoreSubsets := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/rule-503test-destinationrule-c-add-subset.yaml"},
	}

	routeToNewSubsets := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/rule-503test-update-virtualservice.yaml"},
	}

	deleteOldSubsets := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{"testdata/v1alpha3/rule-503test-destinationrule-c-del-subset.yaml"},
	}

	if err := gateway.Setup(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Teardown()

	// Update configs in background
	go func() {
		log.Infof("Adding new subsets v1,v2")
		// these functions have built in sleep. So we don't have to add any extra sleep here
		if err := newDestRule.Setup(); err != nil {
			t.Fatal(err)
		}
		defer newDestRule.Teardown()
		log.Infof("routing to v1/v2")
		if err := newVirtService.Setup(); err != nil {
			t.Fatal(err)
		}
		defer newVirtService.Teardown()
		log.Infof("Adding new subsets v3,v4")
		if err := addMoreSubsets.Setup(); err != nil {
			t.Fatal(err)
		}
		log.Infof("routing to v3,v4")
		if err := routeToNewSubsets.Setup(); err != nil {
			t.Fatal(err)
		}
		log.Infof("deleting old subsets a/b")
		if err := deleteOldSubsets.Setup(); err != nil {
			t.Fatal(err)
		}
	}()

	reqURL := fmt.Sprintf("http://%s.%s/c", ingressGatewayServiceName, istioNamespace)
	resp := ClientRequest("t", reqURL, 100, "-key Host -val uk.bookinfo.com")
	count := make(map[string]int)
	for _, elt := range resp.Code {
		count[elt] = count[elt] + 1
	}
	log.Infof("request counts %v", count)
	if count["200"] != 100 {
			t.Errorf("Got one or more non 200 response codes during the test..")
	}
}