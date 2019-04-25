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
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

const DocFile = "doc.go"

// Doc is the input needed to generate a pkg/apis/<group>/<version>/doc.go file
type Doc struct {
	input.Input

	// Resource defines the inputs for the new doc file
	Resource *Resource
}

func (s *Doc) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = filepath.Join(ApisDir,
			s.Resource.GoImportGroup,
			strings.ToLower(s.Resource.Version),
			DocFile)
	}
	s.IfExistsAction = input.Skip
	s.TemplateBody = docTemplate
	return s.Input, nil
}

const docTemplate = `// Package {{.Resource.Version}} contains API Schema definitions for the {{ .Resource.Group }} {{.Resource.Version}} API group
// +k8s:deepcopy-gen=package,register
// +groupName={{ .Resource.FullGroup }}
package {{.Resource.Version}}
`
