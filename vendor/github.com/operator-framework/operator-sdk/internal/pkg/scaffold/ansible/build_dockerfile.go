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
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/version"
)

const BuildDockerfileFile = "Dockerfile"

type BuildDockerfile struct {
	input.Input
	RolesDir         string
	ImageTag         string
	GeneratePlaybook bool
}

// GetInput - gets the input
func (b *BuildDockerfile) GetInput() (input.Input, error) {
	if b.Path == "" {
		b.Path = filepath.Join(scaffold.BuildDir, BuildDockerfileFile)
	}
	b.TemplateBody = buildDockerfileAnsibleTmpl
	b.RolesDir = RolesDir
	b.ImageTag = strings.TrimSuffix(version.Version, "+git")
	return b.Input, nil
}

const buildDockerfileAnsibleTmpl = `FROM quay.io/operator-framework/ansible-operator:{{.ImageTag}}

COPY watches.yaml ${HOME}/watches.yaml

COPY {{.RolesDir}}/ ${HOME}/{{.RolesDir}}/
{{- if .GeneratePlaybook }}
COPY playbook.yml ${HOME}/playbook.yml
{{- end }}
`
