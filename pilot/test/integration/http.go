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

// Reachability tests

package main

import (
	"fmt"

	proxyconfig "istio.io/api/proxy/v1/config"
)

type http struct {
	*infra
	logs *accessLogs
}

func (r *http) String() string {
	return "http-reachability"
}

func (r *http) setup() error {
	r.logs = makeAccessLogs()
	return nil
}

func (r *http) teardown() {
}

func (r *http) run() error {
	if err := r.makeRequests() {
		return err
	}
	if err := r.logs.check(r.infra) {
		return err
	}
	return nil
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *http) makeRequests() error {
	// Auth is enabled for d:80, and disabled for d:8080 using per-service policy.
	// We expect request from non-envoy client ("t") to d:80 should always fail,
	// while to d:8080 should always success.
	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"a", "b", "d"}
	if r.Auth == proxyconfig.MeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		dstPods = append(dstPods, "t")
		// mTLS is not supported for headless services
		dstPods = append(dstPods, "headless")
	}
	funcs := make(map[string]func() status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			if src == "t" && dst == "t" {
				// this is flaky in minikube
				continue
			}
			for _, port := range []string{"", ":80", ":8080"} {
				for _, domain := range []string{"", "." + r.Namespace} {
					name := fmt.Sprintf("HTTP request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() status {
							resp := r.clientRequest(src, url, 1, "")
							// Auth is enabled for d:80 and disable for d:8080 using per-service
							// policy.
							if src == "t" &&
								((r.Auth == proxyconfig.MeshConfig_MUTUAL_TLS && !(dst == "d" && port == ":8080")) ||
									dst == "d" && (port == ":80" || port == "")) {
								if len(resp.id) == 0 {
									// Expected no match for:
									//   t->a (or b) when auth is on
									//   t->d:80 (all the time)
									// t->d:8000 should always be fine.
									return nil
								}
								return errAgain
							}
							if len(resp.id) > 0 {
								id := resp.id[0]
								if src != "t" {
									r.logs.add(src, id, name)
								}
								if dst != "t" {
									if dst == "headless" { // headless points to b
										if src != "b" {
											r.logs.add("b", id, name)
										}
									} else {
										r.logs.add(dst, id, name)
									}
								}
								// mixer filter is invoked on the server side, that is when dst is not "t"
								if r.Mixer && dst != "t" {
									r.logs.add("mixer", id, name)
								}
								return nil
							}
							if src == "t" && dst == "t" {
								// Expected no match for t->t
								return nil
							}
							return errAgain
						}
					})(src, dst, port, domain)
				}
			}
		}
	}
	return parallel(funcs)
}
