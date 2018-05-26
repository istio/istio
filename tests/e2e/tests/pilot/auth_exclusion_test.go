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

func TestAuthExclusion(t *testing.T) {
	if !tc.Kube.AuthEnabled {
		t.Skipf("Skipping %s: auth_enable=false", t.Name())
	}

	if tc.V1alpha3 {
		// While the mesh config turned on mTLS globally, this DestinationRule lets the client opt-out
		// the mTLS when talking to fake-control service.
		cfgs := &deployableConfig{
			Namespace:  tc.Kube.Namespace,
			YamlFiles:  []string{"testdata/authn/destination-rule-exclude.yaml.tmpl"},
			kubeconfig: tc.Kube.KubeConfig,
		}
		if err := cfgs.Setup(); err != nil {
			t.Fatal(err)
		}
		defer cfgs.Teardown()
	}

	// fake-control service doesn't have sidecar, and is excluded from mTLS so
	// client with sidecar should never use mTLS when talking to it. As the result,
	// all request will works, as if mesh authentication is NONE.
	srcPods := []string{"a", "b", "t"}
	dst := "fake-control"
	ports := []string{"", "80", "8080"}

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for _, src := range srcPods {
			for _, port := range ports {
				for _, domain := range []string{"", "." + tc.Kube.Namespace} {
					testName := fmt.Sprintf("%s->%s%s_%s", src, dst, domain, port)
					runRetriableTest(t, testName, defaultRetryBudget, func() error {
						reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
						resp := ClientRequest(src, reqURL, 1, "")
						// Request should return successfully (status 200)
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
