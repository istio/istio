// Copyright 2018 Istio Authors
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
)

func TestTLSMultiplexing(t *testing.T) {
	if tc.Kube.AuthEnabled {
		t.Skip("multiplexing is used when mesh config auth_enabled is turned off...")
	}
	// This policy will enable mTLS for all namespace, and disable mTLS for c and d:80.
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{
			"testdata/authn/v1alpha1/multiplexing/authn-policy-permissive.yaml",
			"testdata/authn/v1alpha1/multiplexing/destination-rule.yaml",
		},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	// pod a has sidecar, will use DestinationRule `ISTIO_MUTUAL` to send TLS traffic.
	// pod t does not have sidecar, will send plain text traffic.
	srcPods := []string{"a", "t"}
	dstPods := []string{"c"}
	ports := []string{"80"}
	//ports := []string{"", "80", "8080"}

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for cluster := range tc.Kube.Clusters {
		for _, src := range srcPods {
			for _, dst := range dstPods {
				for _, port := range ports {
					for _, domain := range []string{"", "." + tc.Kube.Namespace} {
						testName := fmt.Sprintf("%s->%s%s_%s", src, dst, domain, port)
						runRetriableTest(t, cluster, testName, 15, func() error {
							reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
							resp := ClientRequest(cluster, src, reqURL, 1, "")
							//if src == "t" && (dst == "b" || (dst == "d" && port == "8080")) {
							//	if len(resp.ID) == 0 {
							//		// t cannot talk to b nor d:8080
							//		return nil
							//	}
							//	return errAgain
							//}
							// Request should return successfully (status 200)
							if resp.IsHTTPOk() {
								return nil
							}
							return errAgain
						})
					}
				}
			}
		}
	}})
}
