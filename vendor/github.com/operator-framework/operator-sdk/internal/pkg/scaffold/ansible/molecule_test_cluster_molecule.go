// Copyright 2018 The Operator-SDK Authors
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

package ansible

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const MoleculeTestClusterMoleculeFile = "molecule.yml"

type MoleculeTestClusterMolecule struct {
	input.Input
}

// GetInput - gets the input
func (m *MoleculeTestClusterMolecule) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeTestClusterDir, MoleculeTestClusterMoleculeFile)
	}
	m.TemplateBody = moleculeTestClusterMoleculeAnsibleTmpl

	return m.Input, nil
}

const moleculeTestClusterMoleculeAnsibleTmpl = `---
dependency:
  name: galaxy
driver:
  name: delegated
  options:
    managed: False
    ansible_connection_options: {}
lint:
  name: yamllint
  enabled: False
platforms:
- name: test-cluster
  groups:
  - k8s
provisioner:
  name: ansible
  inventory:
    group_vars:
      all:
        namespace: ${TEST_NAMESPACE:-osdk-test}
  lint:
    name: ansible-lint
    enabled: False
  env:
    ANSIBLE_ROLES_PATH: ${MOLECULE_PROJECT_DIRECTORY}/roles
scenario:
  name: test-cluster
  test_sequence:
    - lint
    - destroy
    - dependency
    - syntax
    - create
    - prepare
    - converge
    - side_effect
    - verify
    - destroy
verifier:
  name: testinfra
  lint:
    name: flake8
`
