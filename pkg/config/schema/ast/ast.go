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

package ast

import (
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config/validation"
	// Force-import a function.
	_ "istio.io/istio/pkg/config/validation/envoyfilter"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/util/strcase"
)

// Metadata is the top-level container.
type Metadata struct {
	Resources []*Resource `json:"resources"`
}

var _ json.Unmarshaler = &Metadata{}

// Resource metadata for resources contained within a collection.
type Resource struct {
	Identifier         string   `json:"identifier"`
	Group              string   `json:"group"`
	Version            string   `json:"version"`
	VersionAliases     []string `json:"versionAliases"`
	Kind               string   `json:"kind"`
	Plural             string   `json:"plural"`
	ClusterScoped      bool     `json:"clusterScoped"`
	Builtin            bool     `json:"builtin"`
	Specless           bool     `json:"specless"`
	Synthetic          bool     `json:"synthetic"`
	Proto              string   `json:"proto"`
	ProtoPackage       string   `json:"protoPackage"`
	StatusProto        string   `json:"statusProto"`
	StatusProtoPackage string   `json:"statusProtoPackage"`
	Validate           string   `json:"validate"`
	Description        string   `json:"description"`
}

// FindResourceForGroupKind looks up a resource with the given group and kind. Returns nil if not found.
func (m *Metadata) FindResourceForGroupKind(group, kind string) *Resource {
	for _, r := range m.Resources {
		if r.Group == group && r.Kind == kind {
			return r
		}
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler
func (m *Metadata) UnmarshalJSON(data []byte) error {
	var in struct {
		Resources []*Resource `json:"resources"`
	}

	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}

	m.Resources = in.Resources
	seen := sets.New[string]()
	// Process resources.
	for i, r := range m.Resources {
		if r.Validate == "" {
			validateFn := "Validate" + asResourceVariableName(r.Kind)
			if !validation.IsValidateFunc(validateFn) {
				validateFn = "validation.EmptyValidate"
			} else {
				if r.Kind == "EnvoyFilter" {
					validateFn = "envoyfilter." + validateFn
				} else {
					validateFn = "validation." + validateFn
				}
			}
			m.Resources[i].Validate = validateFn
		}
		if r.Identifier == "" {
			r.Identifier = r.Kind
		}
		if seen.InsertContains(r.Identifier) {
			return fmt.Errorf("identifier %q already registered, set a unique identifier", r.Identifier)
		}
	}

	return nil
}

// Parse and return a yaml representation of Metadata
func Parse(yamlText string) (*Metadata, error) {
	var s Metadata
	err := yaml.Unmarshal([]byte(yamlText), &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func asResourceVariableName(n string) string {
	return strcase.CamelCase(n)
}
