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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type ingress struct {
	*tutil.Environment
	logs *accessLogs
}

const (
	ingressServiceName = "istio-ingress"
)

func (t *ingress) String() string {
	return "ingress"
}

func (t *ingress) Setup() error {
	if !t.Config.Ingress {
		return nil
	}
	if serviceregistry.ServiceRegistry(t.Config.Registry) != serviceregistry.KubernetesRegistry {
		return nil
	}
	t.logs = makeAccessLogs()

	// parse and send yamls
	if yaml, err := t.Fill("ingress.yaml.tmpl", t.ToTemplateData()); err != nil {
		return err
	} else if err = t.KubeApply(yaml, t.Config.Namespace); err != nil {
		return err
	}

	// send route rules for "c" only
	return t.ApplyConfig("v1alpha1/rule-default-route.yaml.tmpl", nil)
}

func (t *ingress) Run() error {
	if !t.Config.Ingress {
		log.Info("skipping test since ingress is missing")
		return nil
	}
	if serviceregistry.ServiceRegistry(t.Config.Registry) != serviceregistry.KubernetesRegistry {
		return nil
	}

	funcs := make(map[string]func() tutil.Status)
	funcs["Ingress status IP"] = t.checkIngressStatus
	funcs["Route rule for /c"] = t.checkRouteRule

	cases := []struct {
		// empty destination to expect 404
		dst  string
		url  string
		host string
	}{
		{"a", fmt.Sprintf("https://%s.%s:443/http", ingressServiceName, t.Config.IstioNamespace), ""},
		{"b", fmt.Sprintf("https://%s.%s:443/pasta", ingressServiceName, t.Config.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/lucky", ingressServiceName, t.Config.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/.well_known/foo", ingressServiceName, t.Config.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/io.grpc/method", ingressServiceName, t.Config.IstioNamespace), ""},
		{"b", fmt.Sprintf("http://%s.%s/lol", ingressServiceName, t.Config.IstioNamespace), ""},
		{"a", fmt.Sprintf("http://%s.%s/foo", ingressServiceName, t.Config.IstioNamespace), "foo.bar.com"},
		{"a", fmt.Sprintf("http://%s.%s/bar", ingressServiceName, t.Config.IstioNamespace), "foo.baz.com"},
		{"a", fmt.Sprintf("grpc://%s.%s:80", ingressServiceName, t.Config.IstioNamespace), "api.company.com"},
		{"a", fmt.Sprintf("grpcs://%s.%s:443", ingressServiceName, t.Config.IstioNamespace), "api.company.com"},
		{"", fmt.Sprintf("http://%s.%s/notfound", ingressServiceName, t.Config.IstioNamespace), ""},
		{"", fmt.Sprintf("http://%s.%s/foo", ingressServiceName, t.Config.IstioNamespace), ""},
	}
	for _, req := range cases {
		name := fmt.Sprintf("Ingress request to %+v", req)
		funcs[name] = (func(dst, url, host string) func() tutil.Status {
			extra := ""
			if host != "" {
				extra = "-key Host -val " + host
			}
			return func() tutil.Status {
				resp := t.ClientRequest("t", url, 1, extra)
				if dst == "" {
					if len(resp.Code) > 0 && resp.Code[0] == "404" {
						return nil
					}
				} else if len(resp.ID) > 0 {
					if !strings.Contains(resp.Body, "X-Forwarded-For") &&
						!strings.Contains(resp.Body, "x-forwarded-for") {
						log.Warnf("Missing X-Forwarded-For in the body: %s", resp.Body)
						return tutil.ErrAgain
					}

					id := resp.ID[0]
					t.logs.add(dst, id, name)
					t.logs.add("ingress", id, name)
					return nil
				}
				return tutil.ErrAgain
			}
		})(req.dst, req.url, req.host)
	}

	if err := tutil.Parallel(funcs); err != nil {
		return err
	}
	return t.logs.check(t.Environment)
}

// checkRouteRule verifies that version splitting is applied to ingress paths
func (t *ingress) checkRouteRule() tutil.Status {
	url := fmt.Sprintf("http://%s.%s/c", ingressServiceName, t.Config.IstioNamespace)
	resp := t.ClientRequest("t", url, 100, "")
	count := counts(resp.Version)
	log.Infof("counts: %v", count)
	if count["v1"] >= 95 {
		return nil
	}
	return tutil.ErrAgain
}

// ensure that IPs/hostnames are in the ingress statuses
func (t *ingress) checkIngressStatus() tutil.Status {
	ings, err := t.KubeClient.ExtensionsV1beta1().Ingresses(t.Config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(ings.Items) == 0 {
		return fmt.Errorf("ingress status failure: no ingress")
	}
	for _, ing := range ings.Items {
		if len(ing.Status.LoadBalancer.Ingress) == 0 {
			return tutil.ErrAgain
		}

		for _, status := range ing.Status.LoadBalancer.Ingress {
			if status.IP == "" && status.Hostname == "" {
				return tutil.ErrAgain
			}
			log.Infof("Ingress Status IP: %s", status.IP)
		}
	}
	return nil
}

func (t *ingress) Teardown() {
	if !t.Config.Ingress {
		return
	}
	if err := t.DeleteAllConfigs(); err != nil {
		log.Warna(err)
	}
}
