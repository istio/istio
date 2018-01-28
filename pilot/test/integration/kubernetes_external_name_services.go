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

package main

import (
	"fmt"
	"istio.io/istio/pkg/log"
)

type kubernetesExternalNameServices struct {
	*infra
}

func (t *kubernetesExternalNameServices) String() string {
	return "kubernetes-external-name-services"
}

func (t *kubernetesExternalNameServices) setup() error {
	if err := t.applyConfig("rule-rewrite-authority-externalbin.yaml.tmpl", nil); err != nil {
		return err
	}
	return nil
}

func (t *kubernetesExternalNameServices) teardown() {
	log.Info("Cleaning up route rules...")
	if err := t.deleteAllConfigs(); err != nil {
		log.Warna(err)
	}
}

func (t *kubernetesExternalNameServices) run() error {

	srcPods := []string{"a", "b", "t"}
	dstServices := []string{"externalwikipedia", "externalbin"}

	funcs := make(map[string]func() status)
	for _, src := range srcPods {
		for _, dst := range dstServices {
			for _, domain := range []string{"", "." + t.Namespace} {
				name := fmt.Sprintf("HTTP connection from %s to %s%s", src, dst, domain)
				funcs[name] = (func(src, dst, domain string) func() status {
					url := fmt.Sprintf("http://%s%s", dst, domain)
					return func() status {
						resp := t.clientRequest(src, url, 1, "")
						if len(resp.code) > 0 && resp.code[0] == httpOk {
							return nil
						}
						return errAgain
					}
				})(src, dst, domain)
			}
		}
	}
	return parallel(funcs)
}
