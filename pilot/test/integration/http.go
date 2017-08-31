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
	if err := r.makeRequests(); err != nil {
		return err
	}
	if err := r.logs.check(r.infra); err != nil {
		return err
	}
	return nil
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *http) makeRequests() error {
	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"a", "b"}
	if r.Auth == proxyconfig.ProxyMeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		dstPods = append(dstPods, "t")
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
							if r.Auth == proxyconfig.ProxyMeshConfig_MUTUAL_TLS && src == "t" {
								if len(resp.id) == 0 {
									// Expected no match for t->a
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
									r.logs.add(dst, id, name)
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
