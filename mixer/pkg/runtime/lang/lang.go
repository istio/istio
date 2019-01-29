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

// Package lang chooses a language runtime for expressions.
package lang

import (
	"os"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

type (
	// Compiler creates a compiled expression from a string expression
	Compiler interface {
		// Compile creates a compiled expression from a string expression
		Compile(expr string) (compiled.Expression, v1beta1.ValueType, error)
	}
)

var (
	// languageMode selects the evaluation engine for Mixer expressions
	languageMode = os.Getenv("MIXER_LANG_MODE")
)

// NewBuilder returns an expression builder
func NewBuilder(finder ast.AttributeDescriptorFinder) Compiler {
	return compiled.NewBuilder(finder)
}
