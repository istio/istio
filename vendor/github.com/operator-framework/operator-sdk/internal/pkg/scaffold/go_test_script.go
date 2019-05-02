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

const GoTestScriptFile = "go-test.sh"

type GoTestScript struct {
	input.Input
}

func (s *GoTestScript) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = filepath.Join(BuildTestDir, GoTestScriptFile)
	}
	s.IsExec = true
	s.TemplateBody = goTestScriptTmpl
	return s.Input, nil
}

const goTestScriptTmpl = `#!/bin/sh

{{.ProjectName}}-test -test.parallel=1 -test.failfast -root=/ -kubeconfig=incluster -namespacedMan=namespaced.yaml -test.v
`
