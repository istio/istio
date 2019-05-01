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
	"istio.io/istio/tests/util"
)

func TestTcpNonHeadlessPorts(t *testing.T) {
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
							runRetriableTest(t, testName, defaultRetryBudget, func() error {
								reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
								resp := ClientRequest(cluster, src, reqURL, 1, "")
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
		}
	})
}

func TestTCPNonIstioToIstioHeadlessPort(t *testing.T) {
	if tc.Kube.AuthEnabled {
		t.Skipf("Skipping %s: auth_enabled=true", t.Name())
	}

	src := "t"
	dstSvc := "headless"
	port := "10090"

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for cluster := range tc.Kube.Clusters {
			testName := fmt.Sprintf("tcp: %s from %s cluster->%s_%s", src, cluster, dstSvc, port)
			runRetriableTest(t, testName, defaultRetryBudget, func() error {
				reqURL := fmt.Sprintf("http://%s:%s/%s", dstSvc, port, src)
				resp := ClientRequest(cluster, src, reqURL, 1, "")
				if resp.IsHTTPOk() {
					return nil
				}
				return errAgain
			})
		}
	})
}

func TestTcpStatefulSets(t *testing.T) {
	srcPods := []string{"statefulset-0", "statefulset-1"}
	dstPods := []string{"statefulset-0", "statefulset-1"}
	dstService := "statefulset"
	port := "19090"
	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for cluster := range tc.Kube.Clusters {
			for _, src := range srcPods {
				for _, dst := range dstPods {
					if src == dst {
						continue
					}
					// statefulset-1.statefulset.svc.cluster.local
					fqdn := fmt.Sprintf("%s.%s", dst, dstService)
					testName := fmt.Sprintf("tcp: %s from %s cluster->%s_%s", src, cluster, fqdn, port)
					runRetriableTest(t, testName, defaultRetryBudget, func() error {
						reqURL := fmt.Sprintf("http://%s:%s/%s", fqdn, port, src)
						resp := clientRequestFromStatefulSet(cluster, src, reqURL, 1, "")
						if resp.IsHTTPOk() {
							return nil
						}
						return errAgain
					})
				}
			}
		}
	})
}

func clientRequestFromStatefulSet(cluster, pod, url string, count int, extra string) ClientResponse {
	out := ClientResponse{}

	cmd := fmt.Sprintf("client --url %s --count %d %s", url, count, extra)
	request, err := util.PodExec(tc.Kube.Namespace, pod, "app", cmd, true, tc.Kube.Clusters[cluster])
	if err != nil {
		log.Errorf("client request error %v for %s in %s from %s cluster", err, url, pod, cluster)
		return out
	}

	out.Body = request

	ids := idRegex.FindAllStringSubmatch(request, -1)
	for _, id := range ids {
		out.ID = append(out.ID, id[1])
	}

	versions := versionRegex.FindAllStringSubmatch(request, -1)
	for _, version := range versions {
		out.Version = append(out.Version, version[1])
	}

	ports := portRegex.FindAllStringSubmatch(request, -1)
	for _, port := range ports {
		out.Port = append(out.Port, port[1])
	}

	codes := codeRegex.FindAllStringSubmatch(request, -1)
	for _, code := range codes {
		out.Code = append(out.Code, code[1])
	}

	hosts := hostRegex.FindAllStringSubmatch(request, -1)
	for _, host := range hosts {
		out.Host = append(out.Host, host[1])
	}

	return out
}
