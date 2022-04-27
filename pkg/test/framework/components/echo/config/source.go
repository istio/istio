// Copyright Istio Authors
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

package config

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
)

// Source of YAML text.
type Source interface {
	// Template reads the raw input and creates a Template from this source.
	Template() (*param.Template, error)

	// TemplateOrFail calls Template and fails if an error occurs.
	TemplateOrFail(t test.Failer) *param.Template

	// MustTemplate calls Template and panics if an error occurs.
	MustTemplate() *param.Template

	// YAML reads the yaml from this Source. If this source contains parameters,
	// it is evaluated as a template.
	YAML() (string, error)

	// YAMLOrFail calls GetYAML and fails if an error occurs.
	YAMLOrFail(t test.Failer) string

	// MustYAML calls GetYAML and panics if an error occurs.
	MustYAML() string

	// Params returns a copy of the parameters for this Source.
	Params() param.Params

	// WithParams creates a new Source with the given template parameters.
	// If a Source contains params, it will be evaluated as a template by GetYAML.
	// If this source already has parameters, the returned Source will contain
	// a union of the two sets. If the same entry appears in the existing params,
	// the new value overwrites the existing.
	WithParams(params param.Params) Source
}

// YAML returns a Source of raw YAML text.
func YAML(text string) Source {
	return sourceImpl{
		read: func() (string, error) {
			return text, nil
		},
	}
}

// File returns a Source of YAML text stored in files.
func File(filePath string) Source {
	return sourceImpl{
		read: func() (string, error) {
			return file.AsString(filePath)
		},
	}
}

type sourceImpl struct {
	read   func() (string, error)
	params param.Params
}

func (s sourceImpl) Template() (*param.Template, error) {
	raw, err := s.read()
	if err != nil {
		return nil, err
	}

	tpl, err := tmpl.Parse(raw)
	if err != nil {
		return nil, err
	}
	return param.Parse(tpl), nil
}

func (s sourceImpl) TemplateOrFail(t test.Failer) *param.Template {
	t.Helper()
	tpl, err := s.Template()
	if err != nil {
		t.Fatal(err)
	}
	return tpl
}

func (s sourceImpl) MustTemplate() *param.Template {
	tpl, err := s.Template()
	if err != nil {
		panic(err)
	}
	return tpl
}

func (s sourceImpl) YAML() (string, error) {
	// If params were specified, process the yaml as a template.
	if s.params != nil {
		t, err := s.Template()
		if err != nil {
			return "", err
		}
		return tmpl.Execute(t.Template, s.params)
	}

	// Otherwise, just read the yaml.
	return s.read()
}

func (s sourceImpl) YAMLOrFail(t test.Failer) string {
	t.Helper()
	out, err := s.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (s sourceImpl) MustYAML() string {
	out, err := s.YAML()
	if err != nil {
		panic(err)
	}
	return out
}

func (s sourceImpl) Params() param.Params {
	return s.params.Copy()
}

func (s sourceImpl) WithParams(params param.Params) Source {
	if len(params) == 0 {
		return s
	}

	// Merge the parameters (if needed).
	mergedParams := params
	if len(s.params) > 0 {
		// Make a copy of params.
		mergedParams = make(map[string]interface{}, len(params)+len(s.params))
		for k, v := range params {
			mergedParams[k] = v
		}

		// Copy non-conflicting values from this Source.
		for k, v := range s.params {
			if _, ok := mergedParams[k]; !ok {
				mergedParams[k] = v
			}
		}
	}

	return sourceImpl{
		read:   s.read,
		params: mergedParams,
	}
}
