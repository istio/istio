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
	"fmt"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

// AddToScheme is the input needed to generate an addtoscheme_<group>_<version>.go file
type AddToScheme struct {
	input.Input

	// Resource defines the inputs for the new api
	Resource *Resource
}

func (s *AddToScheme) GetInput() (input.Input, error) {
	if s.Path == "" {
		fileName := fmt.Sprintf("addtoscheme_%s_%s.go",
			s.Resource.GoImportGroup,
			strings.ToLower(s.Resource.Version))
		s.Path = filepath.Join(ApisDir, fileName)
	}
	s.TemplateBody = addToSchemeTemplate
	return s.Input, nil
}

const addToSchemeTemplate = `package apis

import (
	"{{ .Repo }}/pkg/apis/{{ .Resource.GoImportGroup}}/{{ .Resource.Version }}"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, {{ .Resource.Version }}.SchemeBuilder.AddToScheme)
}
`
