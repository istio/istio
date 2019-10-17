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

package ast

import (
	"encoding/json"
	"fmt"

	"github.com/ghodss/yaml"
)

// Direct transform's name. Used for parsing.
const Direct = "direct"

// Metadata is the top-level container.
type Metadata struct {
	Collections       []*Collection       `json:"collections"`
	Snapshots         []*Snapshot         `json:"snapshots"`
	Sources           []Source            `json:"sources"`
	TransformSettings []TransformSettings `json:"transforms"`
}

var _ json.Unmarshaler = &Metadata{}

// Collection metadata. Describes basic structure of collections.
type Collection struct {
	Name         string `json:"name"`
	Proto        string `json:"proto"`
	ProtoPackage string `json:"protoPackage"`
}

// Snapshot metadata. Describes the snapshots that should be produced.
type Snapshot struct {
	Name        string   `json:"name"`
	Strategy    string   `json:"strategy"`
	Collections []string `json:"collections"`
}

// Source configuration metadata.
type Source interface {
}

// TransformSettings configuration metadata.
type TransformSettings interface {
	Type() string
}

// KubeSource is configuration for K8s based input sources.
type KubeSource struct {
	Resources []*Resource `json:"resources"`
}

var _ Source = &KubeSource{}

// Resource metadata for a Kubernetes Resource.
type Resource struct {
	Collection    string `json:"collection"`
	Group         string `json:"group"`
	Version       string `json:"version"`
	Kind          string `json:"kind"`
	Plural        string `json:"plural"`
	Disabled      bool   `json:"disabled"`
	ClusterScoped bool   `json:"clusterScoped"`
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

// UnmarshalJSON implements json.Unmarshaler
func (s *Metadata) UnmarshalJSON(data []byte) error {
	var in struct {
		Collections []*Collection     `json:"collections"`
		Snapshots   []*Snapshot       `json:"snapshots"`
		Sources     []json.RawMessage `json:"sources"`
		Transforms  []json.RawMessage `json:"transforms"`
	}

	if err := jsonUnmarshal(data, &in); err != nil {
		return err
	}

	s.Collections = in.Collections
	s.Snapshots = in.Snapshots

	for _, src := range in.Sources {
		m := make(map[string]interface{})
		if err := jsonUnmarshal(src, &m); err != nil {
			return err
		}

		if m["type"] == "kubernetes" {
			ks := &KubeSource{}
			if err := jsonUnmarshal(src, &ks); err != nil {
				return err
			}
			s.Sources = append(s.Sources, ks)
		} else {
			return fmt.Errorf("unable to parse source: %v", string([]byte(src)))
		}
	}

	for _, xform := range in.Transforms {
		m := make(map[string]interface{})
		if err := jsonUnmarshal(xform, &m); err != nil {
			return err
		}

		if m["type"] == Direct {
			dt := &DirectTransformSettings{}
			if err := jsonUnmarshal(xform, &dt); err != nil {
				return err
			}
			s.TransformSettings = append(s.TransformSettings, dt)
		} else {
			return fmt.Errorf("unable to parse transform: %v", string([]byte(xform)))
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
