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
	"log"
)

func TestGrpc(t *testing.T) {
	srcPods := []string{"a", "b"}
	dstPods := []string{"a", "b", "headless"}
	ports := []string{"70", "7070"}
	if !tc.Kube.AuthEnabled {
		// t is not behind proxy, so it cannot talk in Istio auth.
		srcPods = append(srcPods, "t")
		dstPods = append(dstPods, "t")
	} else {
		// Auth is enabled for d:7070 using per-service policy. We expect request
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

	logs := newAccessLogs()

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for cluster := range tc.Kube.Clusters {
			for _, src := range srcPods {
				for _, dst := range dstPods {
					if src == "t" && dst == "t" {
						// this is flaky in minikube
						continue
					}
					for _, port := range ports {
						for _, domain := range []string{"", "." + tc.Kube.Namespace} {
							testName := fmt.Sprintf("%s from %s cluster->%s%s_%s", src, cluster, dst, domain, port)
							runRetriableTest(t, cluster, testName, defaultRetryBudget, func() error {
								reqURL := fmt.Sprintf("grpc://%s%s:%s", dst, domain, port)
								resp := ClientRequest(cluster, src, reqURL, 1, "")
								log.Printf("Debug gRPC src: %s, dst: %s, response: %v", src, dst, resp)
								if len(resp.ID) > 0 {
									id := resp.ID[0]
									logEntry := fmt.Sprintf("GRPC request from %s to %s%s:%s", src, dst, domain, port)
									if src != "t" {
										logs.add(cluster, src, id, logEntry)
									}
									if dst != "t" {
										logs.add(cluster, dst, id, logEntry)
									}
									return nil
								}
								if src == "t" && dst == "t" {
									// Expected no match for t->t
									return nil
								}
								if src == "t" && dst == "d" && port == "7070" {
									// Expected no match for t->d:7070 as d:7070 has mTLS enabled.
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

	log.Print("Debug gRPC dump logs")
	for app, v := range logs.logs {
		log.Printf("  app: %s", app)
		for cluster, requests := range v {
			log.Printf("    cluster: %s", cluster)
			for _, r := range requests {
				log.Printf("      request: %v", r)
			}
		}
	}

	// After all requests complete, run the check logs tests.
	if len(logs.logs) > 0 {
		t.Run("check", func(t *testing.T) {
			logs.checkLogs(t)
		})
	}
}
