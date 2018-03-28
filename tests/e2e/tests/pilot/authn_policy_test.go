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

// Routing tests

package pilot

import (
	"fmt"

	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type authnPolicy struct {
	*tutil.Environment
}

func (r *authnPolicy) String() string {
	return "authn-policy"
}

func (r *authnPolicy) Setup() error {
	return nil
}

func (r *authnPolicy) Teardown() {}

func (r *authnPolicy) Run() error {
	// This authentication policy will:
	// - Enable mTLS for the whole namespace.
	// - But disable mTLS for service c (all ports) and d port 80.
	if err := r.ApplyConfig("v1alpha1/authn-policy.yaml.tmpl", nil); err != nil {
		return err
	}
	return r.makeRequests()
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *authnPolicy) makeRequests() error {
	srcPods := []string{"a", "t"}
	dstPods := []string{"b", "c", "d"}
	funcs := make(map[string]func() tutil.Status)
	// Given the policy, the expected behavior is:
	// - a (with proxy) can talk to all destination that also have proxy, as mTLS will be
	// configured matchingly between them.
	// - t (without proxy) can NOT talk to b and d port 80 as mTLS is on for those destinations.
	// - However, t can still talk to c and d:8080 as mTLS is off for those.
	for _, src := range srcPods {
		for _, dst := range dstPods {
			for _, port := range []string{"", ":80", ":8080"} {
				for _, domain := range []string{"", "." + r.Config.Namespace} {
					name := fmt.Sprintf("Request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() tutil.Status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() tutil.Status {
							resp := r.ClientRequest(src, url, 1, "")
							if src == "t" && (dst == "b" || (dst == "d" && port == ":8080")) {
								if len(resp.ID) == 0 {
									// t cannot talk to b nor d:80
									return nil
								}
								return tutil.ErrAgain
							}
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
	}
	return tutil.Parallel(funcs)
}
