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

package revision

import (
	"fmt"
	"github.com/ghodss/yaml"
	"io/ioutil"
)

// RevisionConfigs carries the Configs for all ASM control plane revisions.
// Revision configuration files are unmarshalled into this struct.
type RevisionConfigs struct {
	Configs []RevisionConfig `json:"revisions"`
}

// RevisionConfig carries config for an ASM control plane revision.
// Tests that require multiple revisions with different configurations or
// versions may use this to configure their SUT.
type RevisionConfig struct {
	Name    string `json:"name"`
	CA      string `json:"ca"`
	Overlay string `json:"overlay"`
}

func ParseRevisionConfig(path string) (*RevisionConfigs, error) {
	yamlContents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read revision config file %q: %w",
			path, err)
	}
	configs := new(RevisionConfigs)
	err = yaml.Unmarshal(yamlContents, configs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal revision config from file %q: %w",
			path, err)
	}
	return configs, nil
}
