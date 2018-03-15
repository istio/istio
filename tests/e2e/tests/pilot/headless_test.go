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

type headless struct {
	*tutil.Environment
}

func (t *headless) String() string {
	return "tcp-headless-reachability"
}

func (t *headless) Setup() error {
	return nil
}

func (t *headless) Teardown() {
}

func (t *headless) Run() error {
	if t.Auth == meshconfig.MeshConfig_MUTUAL_TLS {
		return nil // TODO: mTLS
	}

	srcPods := []string{"a", "b", "t"}
	dstPods := []string{"headless"}
	funcs := make(map[string]func() tutil.Status)
	for _, src := range srcPods {
		for _, dst := range dstPods {
			for _, port := range []string{":10090", ":19090"} {
				for _, domain := range []string{"", "." + t.Config.Namespace} {
					name := fmt.Sprintf("TCP connection from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() tutil.Status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() tutil.Status {
							resp := t.ClientRequest(src, url, 1, "")
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
