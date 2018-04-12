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
)

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
