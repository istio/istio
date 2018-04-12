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

func TestHeadless(t *testing.T) {
	if tc.Kube.AuthEnabled {
		t.Skipf("Skipping %s: auth_enabled=false", t.Name())
	}

	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"headless"}
	ports := []string{"10090", "19090"}

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for _, src := range srcPods {
			for _, dst := range dstPods {
				for _, port := range ports {
					for _, domain := range []string{"", "." + tc.Kube.Namespace} {
						testName := fmt.Sprintf("%s->%s%s_%s", src, dst, domain, port)
						runRetriableTest(t, testName, defaultRetryBudget, func() error {
							reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
							resp := ClientRequest(src, reqURL, 1, "")
							if len(resp.ServerID) > 0 && resp.IsHTTPOk() {
								id := resp.ServerID[0]
								if tc.serverIDMap[dst] != id {
									return errAgain
								}
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
