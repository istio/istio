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
	"istio.io/pilot/platform"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if platform.ServiceRegistry(t.Registry) != platform.KubernetesRegistry {
		return nil
	}
	if _, err := client.CoreV1().Services(t.Namespace).Create(&v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "httpbin",
		},
		Spec: v1.ServiceSpec{
			Type:         "ExternalName",
			ExternalName: "httpbin.org",
			Ports: []v1.ServicePort{{
				Port: 80,
				Name: "http", // important to define protocol
			}},
		},
	}); err != nil {
		return err
	}
	if _, err := client.CoreV1().Services(t.Namespace).Create(&v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "httpsgoogle",
		},
		Spec: v1.ServiceSpec{
			Type:         "ExternalName",
			ExternalName: "cloud.google.com",
			Ports: []v1.ServicePort{{
				Port: 443,
				Name: "https", // important to define protocol
			}},
		},
	}); err != nil {
		return err
	}
	return nil
}

func (t *egress) run() error {
	if !t.Egress {
		glog.Info("skipping test since egress is missing")
		return nil
	}
	if platform.ServiceRegistry(t.Registry) != platform.KubernetesRegistry {
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
