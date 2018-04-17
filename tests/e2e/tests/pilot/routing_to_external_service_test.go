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
	"strings"
	"testing"

	"istio.io/istio/tests/util"
)

func TestExternalServiceRouteFaultInjection(t *testing.T) {
	if !tc.V1alpha3 {
		t.Skipf("Skipping %s: v1alpha3=false", t.Name())
	}

	var cfgs *deployableConfig
	applyRuleFunc := func(t *testing.T, yamlFiles []string) {
		// Delete the previous rule if there was one. No delay on the teardown, since we're going to apply
		// a delay when we push the new config.
		if cfgs != nil {
			if err := cfgs.TeardownNoDelay(); err != nil {
				t.Fatal(err)
			}
			cfgs = nil
		}

		// Apply the new rule
		cfgs = &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  yamlFiles,
			kubeconfig: tc.Kube.KubeConfig,
		}
		if err := cfgs.Setup(); err != nil {
			t.Fatal(err)
		}
	}
	// Upon function exit, delete the active rule.
	defer func() {
		if cfgs != nil {
			_ = cfgs.Teardown()
		}
	}()

	cases := []struct {
		testName              string
		externalServiceConfig string
		routingTemplate       string
		url                   string
		routingParams         map[string]string
	}{
		// Fault Injection
		{
			testName:              "httpbin",
			externalServiceConfig: "testdata/v1alpha3/external-service-httpbin.yaml",
			routingTemplate:       "testdata/v1alpha3/rule-fault-injection-to-external-service.yaml.tmpl",
			routingParams: map[string]string{
				"host": "httpbin.org",
			},
			url: "http://httpbin.org",
		},
		{
			testName:              "*.httpbin",
			externalServiceConfig: "testdata/v1alpha3/external-service-httpbin.yaml",
			routingTemplate:       "testdata/v1alpha3/rule-fault-injection-to-external-service.yaml.tmpl",
			routingParams: map[string]string{
				"host": "*.httpbin.org",
			},
			url: "http://www.httpbin.org",
		},
		{
			testName:              "google",
			externalServiceConfig: "testdata/v1alpha3/external-service-google.yaml",
			routingTemplate:       "testdata/v1alpha3/rule-fault-injection-to-external-service.yaml.tmpl",
			routingParams: map[string]string{
				"host": "*google.com",
			},
			url: "http://www.google.com:443",
		},
	}

	for _, c := range cases {
		// Run case in a function to scope the configuration changes.
		func() {
			// Fill out the routing template
			routingYaml, err := util.CreateAndFill(tc.Info.TempDir, c.routingTemplate, c.routingParams)
			if err != nil {
				t.Fatal(err)
			}

			// Push all of the configs
			applyRuleFunc(t, []string{c.externalServiceConfig, routingYaml})

			runRetriableTest(t, c.testName, 3, func() error {
				resp := ClientRequest("a", c.url, 1, "")

				statusCode := ""
				if len(resp.Code) > 0 {
					statusCode = resp.Code[0]
				}

				expectedRespCode := 418
				if strconv.Itoa(expectedRespCode) != statusCode {
					return fmt.Errorf("fault injection verification failed: status code %s, expected status code %d",
						statusCode, expectedRespCode)
				}
				return nil
			})
		}()
	}
}

func TestExternalServiceAppendHeaders(t *testing.T) {
	if !tc.V1alpha3 {
		t.Skipf("Skipping %s: v1alpha3=false", t.Name())
	}

	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			"testdata/v1alpha3/external-service-httpbin.yaml",
			"testdata/v1alpha3/rule-route-append-headers-httpbin.yaml"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	runRetriableTest(t, "httpbin", 3,
		func() error {
			resp := ClientRequest("a", "http://httpbin.org/headers", 1, "")

			containsAllExpectedHeaders := true

			headers := []string{
				"\"istio-custom-header1\": \"user-defined-value1\"",
				"\"istio-custom-header2\": \"user-defined-value2\""}
			for _, header := range headers {
				if !strings.Contains(strings.ToLower(resp.Body), header) {
					containsAllExpectedHeaders = false
				}
			}

			if !containsAllExpectedHeaders {
				return fmt.Errorf("headers verification failed: headers: %s, expected headers: %s",
					resp.Body, headers)
			}
			return nil
		})

}
