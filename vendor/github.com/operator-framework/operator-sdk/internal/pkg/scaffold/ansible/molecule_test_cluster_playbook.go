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

const MoleculeTestClusterPlaybookFile = "playbook.yml"

type MoleculeTestClusterPlaybook struct {
	input.Input
	Resource scaffold.Resource
}

// GetInput - gets the input
func (m *MoleculeTestClusterPlaybook) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeTestClusterDir, MoleculeTestClusterPlaybookFile)
	}
	m.TemplateBody = moleculeTestClusterPlaybookAnsibleTmpl

	return m.Input, nil
}

const moleculeTestClusterPlaybookAnsibleTmpl = `---

- name: Converge
  hosts: localhost
  connection: local
  vars:
    ansible_python_interpreter: '{{ "{{ ansible_playbook_python }}" }}'
    deploy_dir: "{{"{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') }}/deploy"}}"
    image_name: {{.Resource.FullGroup}}/{{.ProjectName}}:testing
    custom_resource: "{{"{{"}} lookup('file', '/'.join([deploy_dir, 'crds/{{.Resource.Group}}_{{.Resource.Version}}_{{.Resource.LowerKind}}_cr.yaml'])) | from_yaml {{"}}"}}"
  tasks:
  - name: Create the {{.Resource.FullGroup}}/{{.Resource.Version}}.{{.Resource.Kind}}
    k8s:
      namespace: '{{ "{{ namespace }}" }}'
      definition: "{{"{{"}} lookup('file', '/'.join([deploy_dir, 'crds/{{.Resource.Group}}_{{.Resource.Version}}_{{.Resource.LowerKind}}_cr.yaml'])) {{"}}"}}"

  - name: Get the newly created Custom Resource
    debug:
      msg: "{{"{{"}} lookup('k8s', group='{{.Resource.FullGroup}}', api_version='{{.Resource.Version}}', kind='{{.Resource.Kind}}', namespace=namespace, resource_name=custom_resource.metadata.name) {{"}}"}}"

  - name: Wait 40s for reconciliation to run
    k8s_facts:
      api_version: '{{.Resource.Version}}'
      kind: '{{.Resource.Kind }}'
      namespace: '{{"{{"}} namespace {{"}}"}}'
      name: '{{"{{"}} custom_resource.metadata.name {{"}}"}}'
    register: reconcile_cr
    until:
    - "'Successful' in (reconcile_cr | json_query('resources[].status.conditions[].reason'))"
    delay: 4
    retries: 10

- import_playbook: "{{"{{ playbook_dir }}/../default/asserts.yml"}}"
`
