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

//
// Source ------>
//          |
//          |------> outbound listener 8080 (protocol sniffing)
//              |
//              |------> upstream cluster
//                  |
//                  |--------> inbound listener 8080 (or 80) (protocol sniffing)
//                       |
//                       |--------> HTTP filter chain
//                       |
//                       |--------> TCP filter chain
//
//
func TestProtocolSniffing(t *testing.T) {
	srcPods := []string{"a", "t"}
	dstPods := []string{"e"}
	ports := []string{"", "80", "8080"}
	if tc.Kube.AuthEnabled {
		// Auth is enabled for f:80, and disabled for f:8080 using per-service policy.
		// We expect request from non-envoy client ("t") to f:80 should always fail,
		// while to f:8080 should always success.
		cfgs := &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  []string{"testdata/authn/service-f-mtls-policy.yaml.tmpl"},
			kubeconfig: tc.Kube.KubeConfig,
		}
		cfgs.YamlFiles = append(cfgs.YamlFiles, "testdata/authn/destination-rule-f8080.yaml.tmpl")
		if err := cfgs.Setup(); err != nil {
			t.Fatal(err)
		}
		defer cfgs.Teardown()
		dstPods = append(dstPods, "f")
	}

	logs := newAccessLogs()

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for cluster := range tc.Kube.Clusters {
			for _, src := range srcPods {
				for _, dst := range dstPods {
					for _, port := range ports {
						for _, domain := range []string{"", "." + tc.Kube.Namespace} {
							testName := fmt.Sprintf("%s from %s cluster->%s%s_%s", src, cluster, dst, domain, port)
							runRetriableTest(t, testName, defaultRetryBudget, func() error {
								reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
								resp := ClientRequest(cluster, src, reqURL, 1, "")
								// Auth is enabled for f:80 and disable for f:8080 using per-service
								// policy.
								if src == "t" && tc.Kube.AuthEnabled {
									if dst == "f" && (port == "80" || port == "") {
										if len(resp.ID) == 0 {
											return nil
										}

										return errAgain
									}
								}
								logEntry := fmt.Sprintf("HTTP request from %s to %s%s:%s", src, dst, domain, port)
								if len(resp.ID) > 0 {
									id := resp.ID[0]
									if src != "t" {
										logs.add(cluster, src, id, logEntry)
									}

									logs.add(cluster, dst, id, logEntry)
									return nil
								}
								return errAgain
							})
						}
					}
				}
			}
		}
	})

	// After all requests complete, run the check logs tests.
	if len(logs.logs) > 0 {
		t.Run("check", func(t *testing.T) {
			logs.checkLogs(t)
		})
	}
}
