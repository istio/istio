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
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type grpc struct {
	*tutil.Environment
	logs *accessLogs
}

func (t *grpc) String() string {
	return "http2-reachability"
}

func (t *grpc) Setup() error {
	t.logs = makeAccessLogs()
	return nil
}

func (t *grpc) Teardown() {
}

func (t *grpc) Run() error {
	if err := t.makeRequests(); err != nil {
		return err
	}
	return t.logs.check(t.Environment)
}

func (t *grpc) makeRequests() error {
	// Auth is enabled for d:7070 using per-service policy. We expect request
	// from non-envoy client ("t") should fail all the time.
	srcPods := []string{"a", "b"}
	dstPods := []string{"a", "b", "d"}
	if t.Auth == meshconfig.MeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		srcPods = append(srcPods, "t")
		dstPods = append(dstPods, "t")
		// mTLS is not supported for headless services
		dstPods = append(dstPods, "headless")
	}
	funcs := make(map[string]func() tutil.Status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			for _, port := range []string{":70", ":7070"} {
				for _, domain := range []string{"", "." + t.Config.Namespace} {
					name := fmt.Sprintf("GRPC request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() tutil.Status {
						url := fmt.Sprintf("grpc://%s%s%s", dst, domain, port)
						return func() tutil.Status {
							resp := t.ClientRequest(src, url, 1, "")
							if len(resp.ID) > 0 {
								id := resp.ID[0]
								if src != "t" {
									t.logs.add(src, id, name)
								}
								if dst != "t" {
									if dst == "headless" { // headless points to b
										if src != "b" {
											t.logs.add("b", id, name)
										}
									} else {
										t.logs.add(dst, id, name)
									}
								}
								// mixer filter is invoked on the server side, that is when dst is not "t"
								if t.Config.Mixer && dst != "t" {
									t.logs.add("mixer", id, name)
								}
								return nil
							}
							if src == "t" && dst == "t" {
								// Expected no match for t->t
								return nil
							}
							if src == "t" && dst == "d" && port == ":7070" {
								// Expected no match for t->d:7070 as d:7070 has mTLS enabled.
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
