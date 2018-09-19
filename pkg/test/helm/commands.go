//  Copyright 2018 Istio Authors
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

	"istio.io/istio/pkg/test/shell"
)

// Template calls "helm template".
func Template(deploymentName, namespace, chartDir, valuesFile string, s *Settings) (string, error) {
	valuesString := ""
	for k, v := range s.generate() {
		valuesString += fmt.Sprintf(" --set %s=%s", k, v)
	}

	str, err := shell.Execute(
		"helm template %s --name %s --namespace %s --values %s%s",
		chartDir, deploymentName, namespace, valuesFile, valuesString)
	if err == nil {
		return str, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)

}
