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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type tcp struct {
	*tutil.Environment
}

func (t *tcp) String() string {
	return "tcp-reachability"
}

func (t *tcp) Setup() error {
	return nil
}

func (t *tcp) Teardown() {
}

func (t *tcp) Run() error {
	// TCP in Eureka is tested by the headless service test.
	if serviceregistry.ServiceRegistry(t.Config.Registry) == serviceregistry.EurekaRegistry {
		return nil
	}
	// Auth is enabled for d:9090 using per-service policy. We expect request
	// from non-envoy client ("t") should fail all the time.
	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"a", "b", "d"}
	if t.Auth == meshconfig.MeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		dstPods = append(dstPods, "t")
	}
	funcs := make(map[string]func() tutil.Status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			if src == "t" && dst == "t" {
				// this is flaky in minikube
				continue
			}
			for _, port := range []string{":90", ":9090"} {
				for _, domain := range []string{"", "." + t.Config.Namespace} {
					name := fmt.Sprintf("TCP connection from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() tutil.Status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() tutil.Status {
							resp := t.ClientRequest(src, url, 1, "")
							if src == "t" &&
								(t.Auth == meshconfig.MeshConfig_MUTUAL_TLS ||
									(dst == "d" && port == ":9090")) {
								// t cannot talk to envoy (a or b) when mTLS enabled,
								// nor with d:9090 (which always has mTLS enabled).
								if !resp.IsHTTPOk() {
									return nil
								}
							} else if resp.IsHTTPOk() {
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
