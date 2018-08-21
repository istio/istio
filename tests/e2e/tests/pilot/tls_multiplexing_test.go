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

	"istio.io/istio/pkg/log"
)

func TestTLSMultiplexing(t *testing.T) {
	if tc.Kube.AuthEnabled {
		t.Skip("Skipping because multiplexing is used when mesh config auth_enabled is turned off...")
	}

	cfgs := &deployableConfig{
		Namespace: tc.Kube.Namespace,
		YamlFiles: []string{
			// This policy configures b to use PERMISSIVE for port 80 and STRICT for port 8080.
			"testdata/authn/v1alpha1/multiplexing/authn-policy-permissive.yaml",
			// This configure serivce b's client to use ISTIO_MUTUAL mTLS when talking to service c.
			"testdata/authn/v1alpha1/multiplexing/destination-rule.yaml",
		},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	// Pod a has sidecar, will use DestinationRule `ISTIO_MUTUAL` to send TLS traffic.
	// Pod t does not have sidecar, will send plain text traffic.
	srcPods := []string{"a", "t"}
	dstPods := []string{"d"}
	ports := []string{"80", "7070"}
	shouldFails := []struct {
		src  string
		dest string
		port string
	}{
		{"t", "d", "7070"},
	}

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
								expectOK := true
								for _, f := range shouldFails {
									if f.src == src && f.dest == dst && f.port == port {
										expectOK = false
										break
									}
								}
								count := 1
								if !expectOK {
									count = 15
								}
								resp := ClientRequest(cluster, src, reqURL, count, "")
								if resp.IsHTTPOk() && expectOK {
									return nil
								}
								if !resp.IsHTTPOk() && !expectOK {
									return nil
								}
								log.Errorf("multiplex testing failed expect %v, returned http %v", expectOK, resp.IsHTTPOk())
								return errAgain
							})
						}
					}
				}
			}
		}
	})
}
