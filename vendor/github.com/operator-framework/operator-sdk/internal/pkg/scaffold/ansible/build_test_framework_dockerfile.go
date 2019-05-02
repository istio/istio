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

const BuildTestFrameworkDockerfileFile = "Dockerfile"

type BuildTestFrameworkDockerfile struct {
	input.Input
}

// GetInput - gets the input
func (b *BuildTestFrameworkDockerfile) GetInput() (input.Input, error) {
	if b.Path == "" {
		b.Path = filepath.Join(scaffold.BuildTestDir, BuildTestFrameworkDockerfileFile)
	}
	b.TemplateBody = buildTestFrameworkDockerfileAnsibleTmpl

	return b.Input, nil
}

const buildTestFrameworkDockerfileAnsibleTmpl = `ARG BASEIMAGE
FROM ${BASEIMAGE}
USER 0
RUN yum install -y python-devel gcc libffi-devel && pip install molecule
ARG NAMESPACEDMAN
ADD $NAMESPACEDMAN /namespaced.yaml
ADD build/test-framework/ansible-test.sh /ansible-test.sh
RUN chmod +x /ansible-test.sh
USER 1001
ADD . /opt/ansible/project
`
