// Copyright 2019 Istio Authors
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
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
)

const (
	yamlSeparator = "\n---\n"
)

// Split the given yaml doc if it's multipart document.
func Split(yamlText []byte) [][]byte {
	return bytes.Split(yamlText, []byte(yamlSeparator))
}

// SplitString splits the given yaml doc if it's multipart document.
func SplitString(yamlText string) []string {
	return strings.Split(yamlText, yamlSeparator)
}

// Join the given yaml parts into a single multipart document.
func Join(parts ...[]byte) []byte {
	return bytes.Join(parts, []byte(yamlSeparator))
}

// JoinString joins the given yaml parts into a single multipart document.
func JoinString(parts ...string) string {
	return strings.Join(parts, yamlSeparator)
}

// Metadata metadata for a kubernetes resource.
type Metadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Descriptor a descriptor for a kubernetes resource.
type Descriptor struct {
	Kind       string   `json:"kind"`
	Group      string   `json:"group"`
	APIVersion string   `json:"apiVersion"`
	Metadata   Metadata `json:"metadata"`
}

// ParseDescriptor parses the given single-part yaml and generates the descriptor.
func ParseDescriptor(yamlText string) (Descriptor, error) {
	d := Descriptor{}
	jsonText, err := yaml.YAMLToJSON([]byte(yamlText))
	if err != nil {
		return Descriptor{}, fmt.Errorf("failed converting YAML to JSON: %v", err)
	}

	if err := json.Unmarshal(jsonText, &d); err != nil {
		return Descriptor{}, fmt.Errorf("failed parsing descriptor: %v", err)
	}
	return d, nil
}

// ApplyNamespace applies the given namespaces to the resources in the yamlText.
func ApplyNamespace(yamlText, ns string) (string, error) {
	chunks := SplitString(yamlText)

	result := ""
	for _, chunk := range chunks {
		if result != "" {
			result += yamlSeparator
		}
		y, err := applyNamespace(chunk, ns)
		if err != nil {
			return "", err
		}
		result += y
	}

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
