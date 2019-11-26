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
	"io/ioutil"
	"path/filepath"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/tmpl"
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

func (p Path) SelectInput(ctx Context) Input {
	ctx.Helper()
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

func (t Inline) SelectInput(ctx Context) Input {
	ctx.Helper()
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
	ctx.Helper()
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
	ctx.Helper()
	if ctx.KubeEnv().Settings().Minikube {
		return s.Then.SelectInput(ctx)
	}
	return s.Else.SelectInput(ctx)
}

func Evaluate(selector InputSelector, data map[string]interface{}) InputSelector {
	return InputSelectorFunc(func(ctx Context) Input {
		ctx.Helper()

		input := selector.SelectInput(ctx)

		// Read the input template.
		templateContent, err := input.ReadAll()
		if err != nil {
			ctx.Fatalf("failed reading input %s: %v", input.Name(), err)
		}

		// Evaluate the input file as a template.
		output, err := tmpl.Evaluate(templateContent, data)
		if err != nil {
			ctx.Fatalf("failed evaluating template %s: %v", input.Name(), err)
		}

		return Inline{
			FileName: input.Name(),
			Value:    output,
		}
	})
}
