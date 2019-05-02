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

// AddController is the input needed to generate a pkg/controller/add_<kind>.go file
type AddController struct {
	input.Input

	// Resource defines the inputs for the controller's primary resource
	Resource *Resource
}

func (s *AddController) GetInput() (input.Input, error) {
	if s.Path == "" {
		fileName := "add_" + s.Resource.LowerKind + ".go"
		s.Path = filepath.Join(ControllerDir, fileName)
	}
	s.TemplateBody = addControllerTemplate
	return s.Input, nil
}

const addControllerTemplate = `package controller

import (
	"{{ .Repo }}/pkg/controller/{{ .Resource.LowerKind }}"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, {{ .Resource.LowerKind }}.Add)
}
`
