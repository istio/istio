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

	"github.com/golang/glog"

	"istio.io/pilot/model"
	"istio.io/pilot/test/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ingress struct {
	*infra
	logs *accessLogs
}

const (
	ingressServiceName = "istio-ingress"
)

func (t *ingress) String() string {
	return "ingress"
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

	if err := t.applyConfig("rule-default-route.yaml.tmpl", map[string]string{
		"Destination": "c",
		"Namespace":   t.Namespace,
	}, model.RouteRule); err != nil {
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
	funcs["Ingress status IP"] = t.checkIngressStatus
	funcs["Route rule for /c"] = t.checkRouteRule

	cases := []struct {
		dst  string
		path string
		tls  bool
		host string
	}{
		{"a", "/", true, ""},
		{"b", "/pasta", true, ""},
		{"a", "/lucky", false, ""},
		{"b", "/lol", false, ""},
		{"a", "/foo", false, "foo.bar.com"},
		{"a", "/bar", false, "foo.baz.com"},
		// empty destination makes it expect 404
		{"", "/notfound", true, ""},
		{"", "/notfound", false, ""},
		{"", "/foo", false, ""},
	}
	for _, req := range cases {
		name := fmt.Sprintf("Ingress request to %+v", req)
		funcs[name] = (func(dst, path string, tls bool, host string) func() status {
			var url string
			if tls {
				url = fmt.Sprintf("https://%s:443%s", ingressServiceName, path)
			} else {
				url = fmt.Sprintf("http://%s%s", ingressServiceName, path)
			}
			extra := ""
			if host != "" {
				extra = "-key Host -val " + host
			}
			return func() status {
				resp := t.clientRequest("t", url, 1, extra)
				if dst == "" {
					if len(resp.code) > 0 && resp.code[0] == "404" {
						return nil
					}
				} else if len(resp.id) > 0 {
					if !strings.Contains(resp.body, "X-Forwarded-For") {
						glog.Warning("Missing X-Forwarded-For")
						return errAgain
					}

					id := resp.id[0]
					t.logs.add(dst, id, name)
					t.logs.add("ingress", id, name)
					return nil
				}
				return errAgain
			}
		})(req.dst, req.path, req.tls, req.host)
	}

	if err := parallel(funcs); err != nil {
		return err
	}
	if err := t.logs.check(t.infra); err != nil {
		return err
	}
	return nil
}

// checkRouteRule verifies that version splitting is applied to ingress paths
func (t *ingress) checkRouteRule() status {
	url := fmt.Sprintf("http://%s/c", ingressServiceName)
	resp := t.clientRequest("t", url, 100, "")
	count := counts(resp.version)
	glog.V(2).Infof("counts: %v", count)
	if count["v1"] >= 95 {
		return nil
	}
	return errAgain
}

// ensure that IPs/hostnames are in the ingress statuses
func (t *ingress) checkIngressStatus() status {
	ings, err := client.Extensions().Ingresses(t.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(ings.Items) == 0 {
		return fmt.Errorf("ingress status failure: no ingress")
	}
	for _, ing := range ings.Items {
		if len(ing.Status.LoadBalancer.Ingress) == 0 {
			return errAgain
		}

		for _, status := range ing.Status.LoadBalancer.Ingress {
			if status.IP == "" && status.Hostname == "" {
				return errAgain
			}
		}
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
	if err := util.Run("kubectl delete istioconfigs --all -n " + t.Namespace); err != nil {
		glog.Warning(err)
	}
}
