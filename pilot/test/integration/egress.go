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
	"strings"
	"time"

	"github.com/golang/glog"

	"istio.io/manager/test/util"
)

type egress struct {
	*infra
}

func (t *egress) String() string {
	return "egress proxy"
}

func (t *egress) setup() error {
	return nil
}

func (t *egress) run() error {
	if !t.Egress {
		glog.Info("skipping test since egress is missing")
		return nil
	}
	extServices := []string{
		"httpbin",
		//"httpsbin",TODO
	}

	funcs := make(map[string]func() status)
	for _, src := range []string{"a", "b"} {
		for _, dst := range extServices {
			name := fmt.Sprintf("External request from %s to %s", src, dst)
			funcs[name] = (func(src, dst string) func() status {
				url := fmt.Sprintf("http://%s/headers", dst)
				trace := fmt.Sprint(time.Now().UnixNano())
				return func() status {
					resp, err := util.Shell(fmt.Sprintf(
						"kubectl exec %s -n %s -c app -- client -url %s -insecure -key Trace-Id -val %q",
						t.apps[src][0], t.Namespace, url, trace))
					if err != nil {
						glog.Error(err)
						return failure
					}
					if strings.Contains(resp, trace) && strings.Contains(resp, "StatusCode=200") {
						return success
					}
					return again
				}
			})(src, dst)
		}
	}
	return parallel(funcs)
}

func (t *egress) teardown() {
}
