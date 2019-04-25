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
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const PlaybookYamlFile = "playbook.yml"

// Playbook - the playbook tmpl wrapper
type Playbook struct {
	input.Input
	Resource scaffold.Resource
}

// GetInput - gets the input
func (p *Playbook) GetInput() (input.Input, error) {
	if p.Path == "" {
		p.Path = PlaybookYamlFile
	}
	p.TemplateBody = playbookTmpl
	return p.Input, nil
}

const playbookTmpl = `- hosts: localhost
  gather_facts: no
  tasks:
  - import_role:
      name: "{{.Resource.LowerKind}}"
`
