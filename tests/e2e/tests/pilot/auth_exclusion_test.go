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

// Routing tests

package pilot

import (
	"fmt"

	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type authExclusion struct {
	*tutil.Environment
}

func (r *authExclusion) String() string {
	return "auth-exclusion"
}

func (r *authExclusion) Setup() error {
	return nil
}

func (r *authExclusion) Teardown() {}

func (r *authExclusion) Run() error {
	return r.makeRequests()
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *authExclusion) makeRequests() error {
	// fake-control service doesn't have sidecar, and is excluded from mTLS so
	// client with sidecar should never use mTLS when talking to it. As the result,
	// all request will works, as if mesh authentication is NONE.
	srcPods := []string{"a", "b", "t"}
	dst := "fake-control"

	funcs := make(map[string]func() tutil.Status)
	for _, src := range srcPods {
		for _, port := range []string{"", ":80", ":8080"} {
			for _, domain := range []string{"", "." + r.Config.Namespace} {
				name := fmt.Sprintf("Request from %s to %s%s%s", src, dst, domain, port)
				funcs[name] = (func(src, dst, port, domain string) func() tutil.Status {
					url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
					return func() tutil.Status {
						resp := r.ClientRequest(src, url, 1, "")
						// Request should return successfully (status 200)
						if resp.IsHTTPOk() {
							return nil
						}
						return tutil.ErrAgain
					}
				})(src, dst, port, domain)
			}
		}
	}
	return tutil.Parallel(funcs)
}
