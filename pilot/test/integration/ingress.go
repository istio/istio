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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pilot/platform"
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
	if platform.ServiceRegistry(t.Registry) != platform.KubernetesRegistry {
		return nil
	}
	t.logs = makeAccessLogs()

	// parse and send yamls
	if yaml, err := fill("ingress.yaml.tmpl", t.infra); err != nil {
		return err
	} else if err = t.kubeApply(yaml, t.Namespace); err != nil {
		return err
	}

	// send route rules for "c" only
	if err := t.applyConfig("rule-default-route.yaml.tmpl", nil); err != nil {
		return err
	}

	return nil
}

func (t *ingress) run() error {
	if !t.Ingress {
		glog.Info("skipping test since ingress is missing")
		return nil
	}
	if platform.ServiceRegistry(t.Registry) != platform.KubernetesRegistry {
		return nil
	}

	funcs := make(map[string]func() status)
	funcs["Ingress status IP"] = t.checkIngressStatus
	funcs["Route rule for /c"] = t.checkRouteRule

	cases := []struct {
		// empty destination to expect 404
		dst  string
		url  string
		host string
	}{
		{"a", fmt.Sprintf("https://%s.%s:443/http", ingressServiceName, t.IstioNamespace), ""},
		{"b", fmt.Sprintf("https://%s.%s:443/pasta", ingressServiceName, t.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/lucky", ingressServiceName, t.IstioNamespace), ""},
		{"b", fmt.Sprintf("http://%s.%s/lol", ingressServiceName, t.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/foo", ingressServiceName, t.IstioNamespace), "foo.bar.com"},
		{"a", fmt.Sprintf("http://%s.%s/bar", ingressServiceName, t.IstioNamespace), "foo.baz.com"},
		{"a", fmt.Sprintf("grpc://%s.%s:80", ingressServiceName, t.IstioNamespace), "api.company.com"},
		{"a", fmt.Sprintf("grpcs://%s.%s:443", ingressServiceName, t.IstioNamespace), "api.company.com"},
		{"", fmt.Sprintf("http://%s.%s/notfound", ingressServiceName, t.IstioNamespace), ""},
		{"", fmt.Sprintf("http://%s.%s/foo", ingressServiceName, t.IstioNamespace), ""},
	}
	for _, req := range cases {
		name := fmt.Sprintf("Ingress request to %+v", req)
		funcs[name] = (func(dst, url, host string) func() status {
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
					if !strings.Contains(resp.body, "X-Forwarded-For") &&
						!strings.Contains(resp.body, "x-forwarded-for") {
						glog.Warningf("Missing X-Forwarded-For in the body: %s", resp.body)
						return errAgain
					}

					id := resp.id[0]
					t.logs.add(dst, id, name)
					t.logs.add("ingress", id, name)
					return nil
				}
				return errAgain
			}
		})(req.dst, req.url, req.host)
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
	url := fmt.Sprintf("http://%s.%s/c", ingressServiceName, t.IstioNamespace)
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
			glog.Infof("Ingress Status IP: %s", status.IP)
		}
	}
	return nil
}

func (t *ingress) teardown() {
	if !t.Ingress {
		return
	}
	if err := client.Extensions().Ingresses(t.Namespace).
		DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil {
		glog.Warning(err)
	}
	if err := client.CoreV1().Secrets(t.Namespace).
		Delete(ingressSecretName, &metav1.DeleteOptions{}); err != nil {
		glog.Warning(err)
	}
	if err := t.deleteAllConfigs(); err != nil {
		glog.Warning(err)
	}
}
