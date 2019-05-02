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

const MoleculeTestLocalPlaybookFile = "playbook.yml"

type MoleculeTestLocalPlaybook struct {
	input.Input
	Resource scaffold.Resource
}

// GetInput - gets the input
func (m *MoleculeTestLocalPlaybook) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeTestLocalDir, MoleculeTestLocalPlaybookFile)
	}
	m.TemplateBody = moleculeTestLocalPlaybookAnsibleTmpl

	return m.Input, nil
}

const moleculeTestLocalPlaybookAnsibleTmpl = `---

- name: Build Operator in Kubernetes docker container
  hosts: k8s
  vars:
    image_name: {{.Resource.FullGroup}}/{{.ProjectName}}:testing
  tasks:
  # using command so we don't need to install any dependencies
  - name: Get existing image hash
    command: docker images -q {{"{{image_name}}"}}
    register: prev_hash
    changed_when: false

  - name: Build Operator Image
    command: docker build -f /build/build/Dockerfile -t {{"{{ image_name }}"}} /build
    register: build_cmd
    changed_when: not prev_hash.stdout or (prev_hash.stdout and prev_hash.stdout not in ''.join(build_cmd.stdout_lines[-2:]))

- name: Converge
  hosts: localhost
  connection: local
  vars:
    ansible_python_interpreter: '{{ "{{ ansible_playbook_python }}" }}'
    deploy_dir: "{{"{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') }}/deploy"}}"
    pull_policy: Never
    REPLACE_IMAGE: {{.Resource.FullGroup}}/{{.ProjectName}}:testing
    custom_resource: "{{"{{"}} lookup('file', '/'.join([deploy_dir, 'crds/{{.Resource.Group}}_{{.Resource.Version}}_{{.Resource.LowerKind}}_cr.yaml'])) | from_yaml {{"}}"}}"
  tasks:
  - block:
    - name: Delete the Operator Deployment
      k8s:
        state: absent
        namespace: '{{ "{{ namespace }}" }}'
        definition: "{{"{{"}} lookup('template', '/'.join([deploy_dir, 'operator.yaml'])) {{"}}"}}"
      register: delete_deployment
      when: hostvars[groups.k8s.0].build_cmd.changed

    - name: Wait 30s for Operator Deployment to terminate
      k8s_facts:
        api_version: '{{"{{"}} definition.apiVersion {{"}}"}}'
        kind: '{{"{{"}} definition.kind {{"}}"}}'
        namespace: '{{"{{"}} namespace {{"}}"}}'
        name: '{{"{{"}} definition.metadata.name {{"}}"}}'
      vars:
        definition: "{{"{{"}} lookup('template', '/'.join([deploy_dir, 'operator.yaml'])) | from_yaml {{"}}"}}"
      register: deployment
      until: not deployment.resources
      delay: 3
      retries: 10
      when: delete_deployment.changed

    - name: Create the Operator Deployment
      k8s:
        namespace: '{{ "{{ namespace }}" }}'
        definition: "{{"{{"}} lookup('template', '/'.join([deploy_dir, 'operator.yaml'])) {{"}}"}}"

    - name: Create the {{.Resource.FullGroup}}/{{.Resource.Version}}.{{.Resource.Kind}}
      k8s:
        state: present
        namespace: '{{ "{{ namespace }}" }}'
        definition: "{{ "{{ custom_resource }}" }}"

    - name: Wait 40s for reconciliation to run
      k8s_facts:
        api_version: '{{"{{"}} custom_resource.apiVersion {{"}}"}}'
        kind: '{{"{{"}} custom_resource.kind {{"}}"}}'
        namespace: '{{"{{"}} namespace {{"}}"}}'
        name: '{{"{{"}} custom_resource.metadata.name {{"}}"}}'
      register: cr
      until:
      - "'Successful' in (cr | json_query('resources[].status.conditions[].reason'))"
      delay: 4
      retries: 10
    rescue:
    - name: debug cr
      ignore_errors: yes
      failed_when: false
      debug:
        var: debug_cr
      vars:
        debug_cr: '{{"{{"}} lookup("k8s",
          kind=custom_resource.kind,
          api_version=custom_resource.apiVersion,
          namespace=namespace,
          resource_name=custom_resource.metadata.name
        ){{"}}"}}'

    - name: debug memcached lookup
      ignore_errors: yes
      failed_when: false
      debug:
        var: deploy
      vars:
        deploy: '{{"{{"}} lookup("k8s",
          kind="Deployment",
          api_version="apps/v1",
          namespace=namespace,
          label_selector="app=memcached"
        ){{"}}"}}'

    - name: get operator logs
      ignore_errors: yes
      failed_when: false
      command: kubectl logs deployment/{{"{{"}} definition.metadata.name {{"}}"}} -n {{"{{"}} namespace {{"}}"}}
      environment:
        KUBECONFIG: '{{"{{"}} lookup("env", "KUBECONFIG") {{"}}"}}'
      vars:
        definition: "{{"{{"}} lookup('template', '/'.join([deploy_dir, 'operator.yaml'])) | from_yaml {{"}}"}}"
      register: log

    - debug: var=log.stdout_lines

    - fail:
        msg: "Failed on action: converge"

- import_playbook: '{{"{{ playbook_dir }}/../default/asserts.yml"}}'
`
