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

	"github.com/ghodss/yaml"

	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/strcase"
)

// Direct transform's name. Used for parsing.
const Direct = "direct"

// Metadata is the top-level container.
type Metadata struct {
	Collections       []*Collection       `json:"collections"`
	Resources         []*Resource         `json:"resources"`
	Snapshots         []*Snapshot         `json:"snapshots"`
	TransformSettings []TransformSettings `json:"transforms"`
}

var _ json.Unmarshaler = &Metadata{}

// Collection metadata. Describes basic structure of collections.
type Collection struct {
	Name         string `json:"name"`
	VariableName string `json:"variableName"`
	Description  string `json:"description"`
	Group        string `json:"group"`
	Kind         string `json:"kind"`
	Disabled     bool   `json:"disabled"`
	Pilot        bool   `json:"pilot"`
	Deprecated   bool   `json:"deprecated"`
}

// Snapshot metadata. Describes the snapshots that should be produced.
type Snapshot struct {
	Name         string   `json:"name"`
	Strategy     string   `json:"strategy"`
	Collections  []string `json:"collections"`
	VariableName string   `json:"variableName"`
	Description  string   `json:"description"`
}

// TransformSettings configuration metadata.
type TransformSettings interface {
	Type() string
}

// Resource metadata for resources contained within a collection.
type Resource struct {
	Group         string `json:"group"`
	Version       string `json:"version"`
	Kind          string `json:"kind"`
	Plural        string `json:"plural"`
	ClusterScoped bool   `json:"clusterScoped"`
	Proto         string `json:"proto"`
	ProtoPackage  string `json:"protoPackage"`
	Validate      string `json:"validate"`
	Description   string `json:"description"`
}

// DirectTransformSettings configuration
type DirectTransformSettings struct {
	Mapping map[string]string `json:"mapping"`
}

var _ TransformSettings = &DirectTransformSettings{}

// Type implements TransformSettings
func (d *DirectTransformSettings) Type() string {
	return Direct
}

// for testing purposes
var jsonUnmarshal = json.Unmarshal

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
		Collections []*Collection     `json:"collections"`
		Resources   []*Resource       `json:"resources"`
		Snapshots   []*Snapshot       `json:"snapshots"`
		Transforms  []json.RawMessage `json:"transforms"`
	}

	if err := jsonUnmarshal(data, &in); err != nil {
		return err
	}

	m.Collections = in.Collections
	m.Resources = in.Resources
	m.Snapshots = in.Snapshots

	// Parse the transforms manually.
	for _, xform := range in.Transforms {
		rawMap := make(map[string]interface{})
		if err := jsonUnmarshal(xform, &rawMap); err != nil {
			return err
		}

		if rawMap["type"] == Direct {
			dt := &DirectTransformSettings{}
			if err := jsonUnmarshal(xform, &dt); err != nil {
				return err
			}
			m.TransformSettings = append(m.TransformSettings, dt)
		} else {
			return fmt.Errorf("unable to parse transform: %v", string([]byte(xform)))
		}
	}

	// Process resources.
	for i, r := range m.Resources {
		if r.Validate == "" {
			validateFn := "Validate" + asResourceVariableName(r.Kind)
			if !validation.IsValidateFunc(validateFn) {
				validateFn = "EmptyValidate"
			}
			m.Resources[i].Validate = validateFn
		}
	}

	// Process collections.
	for i, c := range m.Collections {
		// If no variable name was specified, use default.
		if c.VariableName == "" {
			m.Collections[i].VariableName = asCollectionVariableName(c.Name)
		}

		if c.Description == "" {
			m.Collections[i].Description = "describes the collection " + c.Name
		}
	}

	// Process snapshots.
	for i, s := range m.Snapshots {
		if s.VariableName == "" {
			m.Snapshots[i].VariableName = asSnapshotVariableName(s.Name)
		}
		if s.Description == "" {
			m.Snapshots[i].Description = "describes the snapshot " + s.Name
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

func asCollectionVariableName(n string) string {
	n = strcase.CamelCaseWithSeparator(n, "/")
	n = strcase.CamelCaseWithSeparator(n, ".")
	return n
}

func asSnapshotVariableName(name string) string {
	return strcase.CamelCase(name)
}
