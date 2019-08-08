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

package conformance

import (
	"encoding/json"
	"io/ioutil"
	"path"

	"sigs.k8s.io/yaml"
)

// MetadataFileName is the canonical name of the metadata file.
const MetadataFileName = "test.yaml"

// Metadata about a single test
type Metadata struct {
	// Name of the test, if specified.
	Name string

	// Skip the test
	Skip bool `json:"skip"`

	// Run tests in exclusive (non-parallel) mode
	Exclusive bool `json:"exclusive"`

	// Labels for this test
	Labels []string `json:"labels"`

	// Environments to use for this test. Empty implies all environments
	Environments []string `json:"environments"`
}

func loadMetadata(dir, name string) (*Metadata, error) {
	p := path.Join(dir, MetadataFileName)
	yamlBytes, err := ioutil.ReadFile(p)
	if err != nil {
		return nil, err
	}

	return parseMetadata(name, yamlBytes)
}

func parseMetadata(name string, yamlBytes []byte) (*Metadata, error) {
	jsonBytes, err := yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return nil, err
	}

	m := &Metadata{}
	if err = json.Unmarshal(jsonBytes, m); err != nil {
		return nil, err
	}

	m.Name = name

	return m, nil
}
