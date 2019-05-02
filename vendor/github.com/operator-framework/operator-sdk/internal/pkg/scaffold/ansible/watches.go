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

const WatchesFile = "watches.yaml"

type Watches struct {
	input.Input
	GeneratePlaybook bool
	RolesDir         string
	Resource         scaffold.Resource
}

// GetInput - gets the input
func (w *Watches) GetInput() (input.Input, error) {
	if w.Path == "" {
		w.Path = WatchesFile
	}
	w.TemplateBody = watchesAnsibleTmpl
	w.RolesDir = RolesDir
	return w.Input, nil
}

const watchesAnsibleTmpl = `---
- version: {{.Resource.Version}}
  group: {{.Resource.FullGroup}}
  kind: {{.Resource.Kind}}
  {{- if .GeneratePlaybook }}
  playbook: /opt/ansible/playbook.yml
  {{- else }}
  role: /opt/ansible/{{.RolesDir}}/{{.Resource.LowerKind}}
  {{- end }}
`
