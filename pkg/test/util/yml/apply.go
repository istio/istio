// Copyright Istio Authors
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

package yml

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/yaml"

	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/tmpl"
)

// ApplyNamespace applies the given namespaces to the resources in the yamlText if not set.
func ApplyNamespace(yamlText, ns string) (string, error) {
	chunks := SplitString(yamlText)

	toJoin := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk, err := applyNamespace(chunk, ns)
		if err != nil {
			return "", err
		}
		toJoin = append(toJoin, chunk)
	}

	result := JoinString(toJoin...)
	return result, nil
}

// ApplyPullSecrets applies the given pullsecret to the deployment resource
func ApplyPullSecret(deploymentYaml string, pullSecret string) (string, error) {
	var deploymentMerge v1.Deployment

	mainYaml, err := yaml.YAMLToJSON([]byte(deploymentYaml))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in base: %s\n%s", err, mainYaml)
	}

	patchYaml := tmpl.MustEvaluate(`
spec:
  template:
    spec:
      imagePullSecrets:
      - name: {{.pullSecret}}  	
`, map[string]string{"pullSecret": pullSecret})

	overlayYaml, err := yaml.YAMLToJSON([]byte(patchYaml))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in overlay: %s\n%s", err, overlayYaml)
	}

	merged, err := strategicpatch.StrategicMergePatch(mainYaml, overlayYaml, &deploymentMerge)
	if err != nil {
		return "", fmt.Errorf("json merge error (%s) for base object: \n%s\n override object: \n%s", err, mainYaml, overlayYaml)
	}

	resYaml, err := yaml.JSONToYAML(merged)
	if err != nil {
		return "", fmt.Errorf("jsonToYAML error (%s) for merged object: \n%s", err, merged)
	}
	return string(resYaml), nil
}

// MustApplyNamespace applies the given namespaces to the resources in the yamlText  if not set.
func MustApplyNamespace(t test.Failer, yamlText, ns string) string {
	y, err := ApplyNamespace(yamlText, ns)
	if err != nil {
		t.Fatalf("ApplyNamespace: %v for text %v", err, yamlText)
	}
	return y
}

func applyNamespace(yamlText, ns string) (string, error) {
	m := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(yamlText), &m); err != nil {
		return "", err
	}

	meta, err := ensureChildMap(m, "metadata")
	if err != nil {
		return "", err
	}
	if meta["namespace"] != nil && meta["namespace"] != "" {
		return yamlText, nil
	}
	meta["namespace"] = ns

	by, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}

	return string(by), nil
}

func ensureChildMap(m map[string]interface{}, name string) (map[string]interface{}, error) {
	c, ok := m[name]
	if !ok {
		c = make(map[string]interface{})
	}

	cm, ok := c.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("child %q field is not a map: %v", name, reflect.TypeOf(c))
	}

	return cm, nil
}
