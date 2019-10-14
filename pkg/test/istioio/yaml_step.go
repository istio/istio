// Copyright 2019 Istio Authors
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

package istioio

import (
	"path"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
)

var _ step = &yamlStep{}

type yamlAction int

const (
	yamlApply yamlAction = iota
	yamlDelete
)

type yamlStep struct {
	fileSelector FileSelector
	namespace    string
	action       yamlAction
}

func newApplyYamlStep(namespace string, fileSelector FileSelector) step {
	return &yamlStep{
		fileSelector: fileSelector,
		namespace:    namespace,
		action:       yamlApply,
	}
}

func newDeleteYamlStep(namespace string, fileSelector FileSelector) step {
	return &yamlStep{
		fileSelector: fileSelector,
		namespace:    namespace,
		action:       yamlDelete,
	}
}

func (s *yamlStep) Run(ctx Context) {
	yamlFile := s.fileSelector.SelectFile(ctx)
	_, filename := path.Split(yamlFile)
	if err := file.Copy(yamlFile, path.Join(ctx.OutputDir, filename)); err != nil {
		ctx.Fatalf("failed copying input files for step: %v", err)
	}

	switch s.action {
	case yamlApply:
		scopes.CI.Infof("Applying %s", yamlFile)
		if err := ctx.Env.Apply(s.namespace, yamlFile); err != nil {
			ctx.Fatalf("failed applying yaml: %v", err)
		}
	default:
		scopes.CI.Infof("Deleting %s", yamlFile)
		if err := ctx.Env.Delete(s.namespace, yamlFile); err != nil {
			ctx.Fatalf("failed deleting yaml: %v", err)
		}
	}
}
