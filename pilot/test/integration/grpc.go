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

package main

import (
	"fmt"

	proxyconfig "istio.io/api/proxy/v1/config"
)

type grpc struct {
	*infra
	logs *accessLogs
}

func (t *grpc) String() string {
	return "http2-reachability"
}

func (t *grpc) setup() error {
	t.logs = makeAccessLogs()
	return nil
}

func (t *grpc) teardown() {
}

func (t *grpc) run() error {
	if err := t.makeRequests(); err != nil {
		return err
	}
	return t.logs.check(t.infra)
}

func (t *grpc) makeRequests() error {
	// Auth is enabled for d:7070 using per-service policy. We expect request
	// from non-envoy client ("t") should fail all the time.
	srcPods := []string{"a", "b"}
	dstPods := []string{"a", "b", "d"}
	if t.Auth == proxyconfig.MeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		srcPods = append(srcPods, "t")
		dstPods = append(dstPods, "t")
		// mTLS is not supported for headless services
		dstPods = append(dstPods, "headless")
	}
	funcs := make(map[string]func() status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			for _, port := range []string{":70", ":7070"} {
				for _, domain := range []string{"", "." + t.Namespace} {
					name := fmt.Sprintf("GRPC request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() status {
						url := fmt.Sprintf("grpc://%s%s%s", dst, domain, port)
						return func() status {
							resp := t.clientRequest(src, url, 1, "")
							if len(resp.id) > 0 {
								id := resp.id[0]
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
								if t.Mixer && dst != "t" {
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
							return errAgain
						}
					})(src, dst, port, domain)
				}
			}
		}
	}
	return parallel(funcs)
}
