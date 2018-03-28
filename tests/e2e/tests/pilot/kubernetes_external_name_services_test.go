// Copyright 2017,2018 Istio Authors
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

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type kubernetesExternalNameServices struct {
	*tutil.Environment
}

func (t *kubernetesExternalNameServices) String() string {
	return "kubernetes-external-name-services"
}

func (t *kubernetesExternalNameServices) Setup() error {
	return t.ApplyConfig("v1alpha1/rule-rewrite-authority-externalbin.yaml.tmpl", nil)
}

func (t *kubernetesExternalNameServices) Teardown() {
	log.Info("Cleaning up route rules...")
	if err := t.DeleteAllConfigs(); err != nil {
		log.Warna(err)
	}
}

func (t *kubernetesExternalNameServices) Run() error {

	// map of source pods to test, to boolean that is true if the pod has Istio proxy
	srcPods := map[string]bool{"a": true, "b": true, "t": false}

	// map of destination external name services to their external hosts
	dstServices := map[string]string{
		"externalwikipedia": "wikipedia.org",
		"externalbin":       "httpbin.org",
	}

	funcs := make(map[string]func() tutil.Status)
	for src, withIstioProxy := range srcPods {
		for dst, externalHost := range dstServices {
			for _, domain := range []string{"", "." + t.Config.Namespace} {
				name := fmt.Sprintf("HTTP connection from %s to %s%s", src, dst, domain)
				funcs[name] = (func(src, dst, domain string) func() tutil.Status {
					url := fmt.Sprintf("http://%s%s", dst, domain)
					extra := ""
					if !withIstioProxy {
						extra = "-key Host -val " + externalHost
					}
					return func() tutil.Status {
						resp := t.ClientRequest(src, url, 1, extra)
						if resp.IsHTTPOk() {
							return nil
						}
						return tutil.ErrAgain
					}
				})(src, dst, domain)
			}
		}
	}
	return tutil.Parallel(funcs)
}
