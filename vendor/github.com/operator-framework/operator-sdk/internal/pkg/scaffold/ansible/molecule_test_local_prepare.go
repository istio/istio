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

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const MoleculeTestLocalPrepareFile = "prepare.yml"

type MoleculeTestLocalPrepare struct {
	input.Input
	Resource scaffold.Resource
}

// GetInput - gets the input
func (m *MoleculeTestLocalPrepare) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeTestLocalDir, MoleculeTestLocalPrepareFile)
	}
	m.TemplateBody = moleculeTestLocalPrepareAnsibleTmpl

	return m.Input, nil
}

const moleculeTestLocalPrepareAnsibleTmpl = `---
- import_playbook: ../default/prepare.yml

- name: Prepare operator resources
  hosts: localhost
  connection: local
  vars:
    ansible_python_interpreter: '{{ "{{ ansible_playbook_python }}" }}'
    deploy_dir: "{{"{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') }}/deploy"}}"
  tasks:
  - name: Create Custom Resource Definition
    k8s:
      definition: "{{"{{"}} lookup('file', '/'.join([deploy_dir, 'crds/{{.Resource.Group}}_{{.Resource.Version}}_{{.Resource.LowerKind}}_crd.yaml'])) {{"}}"}}"

  - name: Ensure specified namespace is present
    k8s:
      api_version: v1
      kind: Namespace
      name: '{{ "{{ namespace }}" }}'

  - name: Create RBAC resources
    k8s:
      definition: "{{"{{"}} lookup('template', '/'.join([deploy_dir, item])) {{"}}"}}"
      namespace: '{{ "{{ namespace }}" }}'
    with_items:
      - role.yaml
      - role_binding.yaml
      - service_account.yaml
`
