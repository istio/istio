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

	"github.com/ghodss/yaml"
)

// ApplyNamespace applies the given namespaces to the resources in the yamlText.
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

func applyNamespace(yamlText, ns string) (string, error) {
	m := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(yamlText), &m); err != nil {
		return "", err
	}

	meta, err := ensureChildMap(m, "metadata")
	if err != nil {
		return "", err
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
