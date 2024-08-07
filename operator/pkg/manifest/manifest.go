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

package manifest

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/pkg/test/util/yml"
)

type Manifest struct {
	*unstructured.Unstructured
	Content string
}

func ObjectHash(o *unstructured.Unstructured) string {
	k := o.GroupVersionKind().Kind
	switch o.GroupVersionKind().Kind {
	case "ClusterRole", "ClusterRoleBinding":
		return k + ":" + o.GetName()
	}
	return k + ":" + o.GetNamespace() + ":" + o.GetName()
}

func (m Manifest) Hash() string {
	return ObjectHash(m.Unstructured)
}

func FromYaml(y []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(y, us); err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(y),
	}, nil
}

func FromJSON(j []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := json.Unmarshal(j, us); err != nil {
		return Manifest{}, err
	}
	yml, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(yml),
	}, nil
}

func FromObject(us *unstructured.Unstructured) (Manifest, error) {
	c, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(c),
	}, nil
}

// ParseMultiple splits a string containing potentially many YAML objects, and parses them
func ParseMultiple(output string) ([]Manifest, error) {
	return Parse(yml.SplitString(output))
}

// Parse parses a list of YAML objects
func Parse(output []string) ([]Manifest, error) {
	res := make([]Manifest, 0, len(output))
	for _, m := range output {
		mf, err := FromYaml([]byte(m))
		if err != nil {
			return nil, err
		}
		if mf.GetObjectKind().GroupVersionKind().Kind == "" {
			// This is not an object. Could be empty template, comments only, etc
			continue
		}
		res = append(res, mf)
	}
	return res, nil
}

type ManifestSet struct {
	Component component.Name
	Manifests []Manifest
	// TODO: notes, warnings, etc?
}

func ExtractComponent(sets []ManifestSet, c component.Name) []Manifest {
	for _, m := range sets {
		if m.Component == c {
			return m.Manifests
		}
	}
	return nil
}
