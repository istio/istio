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

const MoleculeDefaultMoleculeFile = "molecule.yml"

type MoleculeDefaultMolecule struct {
	input.Input
}

// GetInput - gets the input
func (m *MoleculeDefaultMolecule) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeDefaultDir, MoleculeDefaultMoleculeFile)
	}
	m.TemplateBody = moleculeDefaultMoleculeAnsibleTmpl

	return m.Input, nil
}

const moleculeDefaultMoleculeAnsibleTmpl = `---
dependency:
  name: galaxy
driver:
  name: docker
lint:
  name: yamllint
  enabled: False
platforms:
- name: kind-default
  groups:
  - k8s
  image: bsycorp/kind:latest-1.12
  privileged: True
  override_command: no
  exposed_ports:
    - 8443/tcp
    - 10080/tcp
  published_ports:
    - 0.0.0.0:${TEST_CLUSTER_PORT:-9443}:8443/tcp
  pre_build_image: yes
provisioner:
  name: ansible
  log: True
  lint:
    name: ansible-lint
    enabled: False
  inventory:
    group_vars:
      all:
        namespace: ${TEST_NAMESPACE:-osdk-test}
  env:
    K8S_AUTH_KUBECONFIG: /tmp/molecule/kind-default/kubeconfig
    KUBECONFIG: /tmp/molecule/kind-default/kubeconfig
    ANSIBLE_ROLES_PATH: ${MOLECULE_PROJECT_DIRECTORY}/roles
    KIND_PORT: '${TEST_CLUSTER_PORT:-9443}'
scenario:
  name: default
verifier:
  name: testinfra
  lint:
    name: flake8
`
