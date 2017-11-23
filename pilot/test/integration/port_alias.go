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

package main

import (
	"fmt"

	proxyconfig "istio.io/api/proxy/v1/config"
)

type portAlias struct {
	*infra
}

func (r *portAlias) String() string {
	return "port-alias"
}

func (r *portAlias) setup() error {
	return nil
}

func (r *portAlias) teardown() {}

func (r *portAlias) run() error {
	if err := r.makeRequests(); err != nil {
		return err
	}
	return nil
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *portAlias) makeRequests() error {
	// 10080, 10081, 10082 are alias port of port 80. 10080 use the authentication
	// policy defined in the mesh config, while 10081 and 10082 authentication
	// policy are set in service-level to 'NONE' and 'MUTUAL_TLS' respectively.
	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"a", "b", "t"}

	funcs := make(map[string]func() status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			for _, port := range []string{":10080", ":10081", ":10082"} {
				for _, domain := range []string{"", "." + r.Namespace} {
					name := fmt.Sprintf("HTTP Request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() status {
							resp := r.clientRequest(src, url, 1, "")
							if dst == "t" {
								if len(resp.id) == 0 {
									// Withouth sidecar, these port mapped to non-exist endpoint,
									// so should fail.
									return nil
								}
								return errAgain
							}
							if src == "t" &&
								(port == ":10082" || (r.Auth == proxyconfig.MeshConfig_MUTUAL_TLS && port == ":10080")) {
								// When auth is enable, non-envoy service t cannot connect.
								if len(resp.id) == 0 {
									return nil
								}
								return errAgain
							}
							// Otherwise, request should return successfully (status 200)
							if len(resp.code) > 0 && resp.code[0] == "200" {
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
