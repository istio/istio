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

const RolesTemplatesDir = "templates" + filePathSep + ".placeholder"

type RolesTemplates struct {
	input.Input
	Resource scaffold.Resource
}

// GetInput - gets the input
func (r *RolesTemplates) GetInput() (input.Input, error) {
	if r.Path == "" {
		r.Path = filepath.Join(RolesDir, r.Resource.LowerKind, RolesTemplatesDir)
	}
	r.TemplateBody = rolesTemplatesDirPlaceholder
	return r.Input, nil
}

const rolesTemplatesDirPlaceholder = ``
