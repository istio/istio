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

const MoleculeDefaultPlaybookFile = "playbook.yml"

type MoleculeDefaultPlaybook struct {
	input.Input
	GeneratePlaybook bool
	Resource         scaffold.Resource
}

// GetInput - gets the input
func (m *MoleculeDefaultPlaybook) GetInput() (input.Input, error) {
	if m.Path == "" {
		m.Path = filepath.Join(MoleculeDefaultDir, MoleculeDefaultPlaybookFile)
	}
	m.TemplateBody = moleculeDefaultPlaybookAnsibleTmpl

	return m.Input, nil
}

const moleculeDefaultPlaybookAnsibleTmpl = `---
{{- if .GeneratePlaybook }}
- import_playbook: '{{"{{ playbook_dir }}/../../playbook.yml"}}'
{{- end }}

  {{- if not .GeneratePlaybook }}
- name: Converge
  hosts: localhost
  connection: local
  vars:
    ansible_python_interpreter: '{{ "{{ ansible_playbook_python }}" }}'
  roles:
    - {{.Resource.LowerKind}}
  {{- end }}

- import_playbook: '{{"{{ playbook_dir }}/asserts.yml"}}'
`
