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
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const WatchesYamlFile = "watches.yaml"

// WatchesYAML specifies the Helm watches.yaml manifest scaffold
type WatchesYAML struct {
	input.Input

	Resource      *scaffold.Resource
	HelmChartsDir string
	ChartName     string
}

// GetInput gets the scaffold execution input
func (s *WatchesYAML) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = WatchesYamlFile
	}
	s.HelmChartsDir = HelmChartsDir
	s.TemplateBody = watchesYAMLTmpl
	if s.ChartName == "" {
		s.ChartName = s.Resource.LowerKind
	}
	return s.Input, nil
}

const watchesYAMLTmpl = `---
- version: {{.Resource.Version}}
  group: {{.Resource.FullGroup}}
  kind: {{.Resource.Kind}}
  chart: /opt/helm/{{.HelmChartsDir}}/{{.ChartName}}
`
