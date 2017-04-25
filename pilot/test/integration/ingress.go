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

	"github.com/golang/glog"

	"istio.io/manager/test/util"
)

type ingress struct {
	*infra
	logs *accessLogs
}

const (
	ingressServiceName = "istio-ingress"
)

func (t *ingress) String() string {
	return "ingress controller"
}

func (t *ingress) setup() error {
	if !t.Ingress {
		return nil
	}
	t.logs = makeAccessLogs()

	// setup ingress resources
	if err := util.Run(fmt.Sprintf("kubectl -n %s create secret generic ingress "+
		"--from-file=tls.key=test/integration/testdata/cert.key "+
		"--from-file=tls.crt=test/integration/testdata/cert.crt",
		t.Namespace)); err != nil {
		return err
	}

	if err := util.Run(fmt.Sprintf(
		"kubectl -n %s apply -f test/integration/testdata/ingress.yaml", t.Namespace)); err != nil {
		return err
	}

	return nil
}

func (t *ingress) run() error {
	if !t.Ingress {
		glog.Info("skipping test since ingress is missing")
		return nil
	}

	funcs := make(map[string]func() status)
	cases := []struct {
		dst  string
		path string
		tls  bool
	}{
		{"a", "/", true},
		{"b", "/pasta", true},
		{"a", "/lucky", false},
		{"b", "/lol", false},
		// empty destination makes it expect 404
		{"", "/notfound", true},
		{"", "/notfound", false},
	}
	for _, req := range cases {
		name := fmt.Sprintf("Ingress request to %+v", req)
		funcs[name] = (func(dst, path string, tls bool) func() status {
			var url string
			if tls {
				url = fmt.Sprintf("https://%s:443%s", ingressServiceName, path)
			} else {
				url = fmt.Sprintf("http://%s%s", ingressServiceName, path)
			}
			return func() status {
				resp := t.clientRequest("t", url, 1, "")
				if dst == "" {
					if len(resp.code) > 0 && resp.code[0] == "404" {
						return nil
					}
				} else if len(resp.id) > 0 {
					id := resp.id[0]
					t.logs.add(dst, id, name)
					t.logs.add("ingress", id, name)
					return nil
				}
				return errAgain
			}
		})(req.dst, req.path, req.tls)
	}

	if err := parallel(funcs); err != nil {
		return err
	}
	if err := t.logs.check(t.infra); err != nil {
		return err
	}
	return nil
}

func (t *ingress) teardown() {
	if !t.Ingress {
		return
	}
	if err := util.Run("kubectl delete secret ingress -n " + t.Namespace); err != nil {
		glog.Warning(err)
	}
	if err := util.Run("kubectl delete ingress --all -n " + t.Namespace); err != nil {
		glog.Warning(err)
	}
}
