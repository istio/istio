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
	"fmt"
	"io/ioutil"
	"path/filepath"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
)

type Input interface {
	InputSelector
	Name() string
	ReadAll() (string, error)
}

type InputSelector interface {
	SelectInput(Context) Input
}

var _ Input = Path("")
var _ InputSelector = Path("")

// TODO(nmittler): Rename to File
type Path string

func (p Path) Name() string {
	return string(p)
}

func (p Path) ReadAll() (string, error) {
	content, err := ioutil.ReadFile(string(p))
	return string(content), err
}

func (p Path) SelectInput(Context) Input {
	return p
}

var _ Input = Inline{}
var _ InputSelector = Inline{}

type Inline struct {
	FileName string
	Value    string
}

func (t Inline) Name() string {
	return t.FileName
}

func (t Inline) ReadAll() (string, error) {
	return t.Value, nil
}

func (t Inline) SelectInput(Context) Input {
	return t
}

func BookInfo(relativePath string) Input {
	return Path(filepath.Join(env.IstioSrc, "samples/bookinfo/platform/kube/"+relativePath))
}

func InputSelectorFunc(fn func(ctx Context) Input) InputSelector {
	return &inputSelector{fn: fn}
}

type inputSelector struct {
	fn func(Context) Input
}

func (s *inputSelector) SelectInput(ctx Context) Input {
	return s.fn(ctx)
}

var _ InputSelector = IfMinikube{}

// IfMinikube is a FileSelector that chooses Input based on whether the environment is configured for Minikube.
type IfMinikube struct {
	// Then is selected when the environment is configured for Minikube.
	Then InputSelector
	// Else is selected whtn the environment is NOT configured for Minikube.
	Else InputSelector
}

func (s IfMinikube) SelectInput(ctx Context) Input {
	if ctx.Env.Settings().Minikube {
		return s.Then.SelectInput(ctx)
	}
	return s.Else.SelectInput(ctx)
}

func Evaluate(selector InputSelector, data map[string]interface{}) InputSelector {
	return InputSelectorFunc(func(ctx Context) Input {
		ctx.Helper()

		input := selector.SelectInput(ctx)

		// Read the input file.
		content, err := input.ReadAll()
		if err != nil {
			ctx.Fatalf("failed reading input %s: %v", input.Name(), err)
		}

		// Evaluate the input file as a template.
		output, err := tmpl.Evaluate(content, data)
		if err != nil {
			ctx.Fatalf("failed evaluating template %s: %v", input.Name(), err)
		}

		// Generate a file with a dynamically assigned name.
		f, err := ioutil.TempFile(ctx.WorkDir(), fmt.Sprintf("*-%s", filepath.Base(input.Name())))
		if err != nil {
			ctx.Fatalf("failed creating template output for %s: %v", input.Name(), err)
		}
		_ = f.Close()

		fullPath := filepath.Join(ctx.WorkDir(), f.Name())

		// Write the content to the file.
		if err := ioutil.WriteFile(fullPath, []byte(output), 0644); err != nil {
			ctx.Fatalf("failed writing template output for %s: %v", input.Name(), err)
		}

		return Path(fullPath)
	})
}

func YamlResource(resourceName string, selector InputSelector) InputSelector {
	return InputSelectorFunc(func(ctx Context) Input {
		ctx.Helper()

		input := selector.SelectInput(ctx)

		content, err := input.ReadAll()
		if err != nil {
			ctx.Fatalf("failed reading YAML input %s: %v", input.Name(), err)
		}

		parts, err := yml.Parse(content)
		if err != nil {
			ctx.Fatalf("failed parsing YAML input %s: %v", input.Name(), err)
		}

		found := false
		for _, part := range parts {
			if part.Descriptor.Metadata.Name == resourceName {
				found = true
				content = part.Contents
				break
			}
		}

		if !found {
			ctx.Fatalf("failed to find YAML resource %s from input %s", resourceName, input.Name)
		}

		return Inline{
			FileName: fmt.Sprintf("%s_%s.yaml", input.Name(), resourceName),
			Value:    content,
		}
	})
}
