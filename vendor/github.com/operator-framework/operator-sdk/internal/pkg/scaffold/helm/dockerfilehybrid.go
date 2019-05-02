// Copyright 2019 The Operator-SDK Authors
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

package helm

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

//DockerfileHybrid - Dockerfile for a hybrid operator
type DockerfileHybrid struct {
	input.Input

	// HelmCharts - if true, include a COPY statement for the helm-charts directory
	HelmCharts bool

	// Watches - if true, include a COPY statement for watches.yaml
	Watches bool
}

// GetInput - gets the input
func (d *DockerfileHybrid) GetInput() (input.Input, error) {
	if d.Path == "" {
		d.Path = filepath.Join(scaffold.BuildDir, scaffold.DockerfileFile)
	}
	d.TemplateBody = dockerFileHybridHelmTmpl
	return d.Input, nil
}

const dockerFileHybridHelmTmpl = `FROM registry.access.redhat.com/ubi7-dev-preview/ubi-minimal:7.6

ENV OPERATOR=/usr/local/bin/helm-operator \
    USER_UID=1001 \
    USER_NAME=helm \
    HOME=/opt/helm

{{- if .Watches }}
COPY watches.yaml ${HOME}/watches.yaml{{ end }}

# install operator binary
COPY build/_output/bin/{{.ProjectName}} ${OPERATOR}

COPY bin /usr/local/bin
RUN  /usr/local/bin/user_setup

{{- if .HelmCharts }}
COPY helm-charts/ ${HOME}/helm-charts/{{ end }}

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}`
