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
)

func TestTcp(t *testing.T) {
	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"a", "b"}
	ports := []string{"90", "9090"}
	if !tc.Kube.AuthEnabled {
		// t is not behind proxy, so it cannot talk in Istio auth.
		dstPods = append(dstPods, "t")
	} else {
		// Auth is enabled for d:9090 using per-service policy. We expect request
		// from non-envoy client ("t") should fail all the time.
		cfgs := &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  []string{"testdata/authn/service-d-mtls-policy.yaml.tmpl"},
			kubeconfig: tc.Kube.KubeConfig,
		}
		if err := cfgs.Setup(); err != nil {
			t.Fatal(err)
		}
		defer cfgs.Teardown()
		dstPods = append(dstPods, "d")
	}

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for _, src := range srcPods {
			for _, dst := range dstPods {
				if src == "t" && dst == "t" {
					// this is flaky in minikube
					continue
				}
				for _, port := range ports {
					for _, domain := range []string{"", "." + tc.Kube.Namespace} {
						testName := fmt.Sprintf("%s->%s%s_%s", src, dst, domain, port)
						runRetriableTest(t, testName, defaultRetryBudget, func() error {
							reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
							resp := ClientRequest(src, reqURL, 1, "")
							if src == "t" && (tc.Kube.AuthEnabled || (dst == "d" && port == "9090")) {
								// t cannot talk to envoy (a or b) when mTLS enabled,
								// nor with d:9090 (which always has mTLS enabled).
								if !resp.IsHTTPOk() {
									return nil
								}
							} else if resp.IsHTTPOk() {
								return nil
							}
							return errAgain
						})
					}
				}
			}
		}
	})
}
