//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package helm

import (
	"fmt"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
)

// Init calls "helm init"
func Init(homeDir string, clientOnly bool) error {
	clientSuffix := ""
	if clientOnly {
		clientSuffix = " --client-only"
	}

	out, err := shell.Execute("helm --home %s init %s", homeDir, clientSuffix)
	if err != nil {
		scopes.Framework.Errorf("helm init: %v, out:%q", err, out)
	} else {
		scopes.CI.Infof("helm init:\n%s\n", out)
	}

	return err
}

func Template(homeDir, template, name, namespace string, valuesFile string, values map[string]string) (string, error) {
	p := []string{"helm", "--home", homeDir, "template", template, "--name", name, "--namespace", namespace}
	if valuesFile != "" {
		p = append(p, "--values", valuesFile)
	}

	// Override the values in the helm value file.
	for k, v := range values {
		if k == "" {
			continue
		}
		p = append(p, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	out, err := shell.ExecuteArgs(nil, "helm", p[1:]...)
	if err != nil {
		scopes.Framework.Errorf("helm template: %v, out:%q", err, out)
	}

	return out, err
}
