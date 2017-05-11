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
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/manager/test/util"
)

type egress struct {
	*infra
}

func (t *egress) String() string {
	return "egress"
}

func (t *egress) setup() error {
	if !t.Egress {
		return nil
	}
	if err := util.Run(fmt.Sprintf(
		"kubectl -n %s apply -f test/integration/testdata/external-service.yaml", t.Namespace)); err != nil {
		return err
	}
	return nil
}

func (t *egress) run() error {
	if !t.Egress {
		glog.Info("skipping test since egress is missing")
		return nil
	}
	extServices := map[string]string{
		"httpbin":         "/headers",
		"httpsgoogle:443": "",
	}

	funcs := make(map[string]func() status)
	for _, src := range []string{"a", "b"} {
		for dst, path := range extServices {
			name := fmt.Sprintf("External request from %s to %s", src, dst)
			funcs[name] = (func(src, dst string) func() status {
				url := fmt.Sprintf("http://%s%s", dst, path)
				trace := fmt.Sprint(time.Now().UnixNano())
				return func() status {
					resp := t.clientRequest(src, url, 1, fmt.Sprintf("-key Trace-Id -val %q", trace))
					if len(resp.code) > 0 && resp.code[0] == httpOk && strings.Contains(resp.body, trace) {
						return nil
					}
					return errAgain
				}
			})(src, dst)
		}
	}
	return parallel(funcs)
}

func (t *egress) teardown() {
	if !t.Egress {
		return
	}
	if err := client.CoreV1().Services(t.Namespace).Delete("httpbin", &meta_v1.DeleteOptions{}); err != nil {
		glog.Warning(err)
	}
	if err := client.CoreV1().Services(t.Namespace).Delete("httpsgoogle", &meta_v1.DeleteOptions{}); err != nil {
		glog.Warning(err)
	}
}
