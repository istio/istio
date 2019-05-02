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

package scaffold

import (
	"path/filepath"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const ServiceAccountYamlFile = "service_account.yaml"

type ServiceAccount struct {
	input.Input
}

func (s *ServiceAccount) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = filepath.Join(DeployDir, ServiceAccountYamlFile)
	}
	s.TemplateBody = serviceAccountTemplate
	return s.Input, nil
}

const serviceAccountTemplate = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{.ProjectName}}
`
