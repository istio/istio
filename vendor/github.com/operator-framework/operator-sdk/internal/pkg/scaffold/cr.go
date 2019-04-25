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
	"text/template"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

// CR is the input needed to generate a deploy/crds/<group>_<version>_<kind>_cr.yaml file
type CR struct {
	input.Input

	// Resource defines the inputs for the new custom resource
	Resource *Resource

	// Spec is a custom spec for the CR. It will be automatically indented. If
	// unset, a default spec will be created.
	Spec string
}

func (s *CR) GetInput() (input.Input, error) {
	if s.Path == "" {
		fileName := fmt.Sprintf("%s_%s_%s_cr.yaml",
			strings.ToLower(s.Resource.Group),
			strings.ToLower(s.Resource.Version),
			s.Resource.LowerKind)
		s.Path = filepath.Join(CRDsDir, fileName)
	}
	s.TemplateBody = crTemplate
	if s.TemplateFuncs == nil {
		s.TemplateFuncs = template.FuncMap{}
	}
	s.TemplateFuncs["indent"] = indent
	return s.Input, nil
}

func indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces)
	return pad + strings.Replace(v, "\n", "\n"+pad, -1)
}

const crTemplate = `apiVersion: {{ .Resource.APIVersion }}
kind: {{ .Resource.Kind }}
metadata:
  name: example-{{ .Resource.LowerKind }}
spec:
{{- with .Spec }}
{{ . | indent 2 }}
{{- else }}
  # Add fields here
  size: 3
{{- end }}
`
