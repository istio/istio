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

package helm

import (
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/version"
)

// Dockerfile specifies the Helm Dockerfile scaffold
type Dockerfile struct {
	input.Input

	HelmChartsDir string
	ImageTag      string
}

// GetInput gets the scaffold execution input
func (d *Dockerfile) GetInput() (input.Input, error) {
	if d.Path == "" {
		d.Path = filepath.Join(scaffold.BuildDir, scaffold.DockerfileFile)
	}
	d.HelmChartsDir = HelmChartsDir
	d.ImageTag = strings.TrimSuffix(version.Version, "+git")
	d.TemplateBody = dockerFileHelmTmpl
	return d.Input, nil
}

const dockerFileHelmTmpl = `FROM quay.io/operator-framework/helm-operator:{{.ImageTag}}

COPY watches.yaml ${HOME}/watches.yaml
COPY {{.HelmChartsDir}}/ ${HOME}/{{.HelmChartsDir}}/
`
