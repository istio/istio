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
	"istio.io/istio/mixer/pkg/lang/cel"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

type (
	// Compiler creates a compiled expression from a string expression
	Compiler interface {
		// Compile creates a compiled expression from a string expression
		Compile(expr string) (compiled.Expression, v1beta1.ValueType, error)
	}

	// LanguageRuntime enumerates the expression languages supported by istio
	LanguageRuntime int
)

const (
	// CEXL is legacy istio expression language
	CEXL LanguageRuntime = iota

	// CEL is Common Expression Language (https://github.com/google/cel-spec)
	CEL

	// COMPAT is a hybrid with CEXL syntax but CEL semantics
	COMPAT

	// LanguageRuntimeAnnotation on config resources to select a language runtime
	LanguageRuntimeAnnotation = "policy.istio.io/lang"
)

// GetLanguageRuntime reads an override from a resource annotation
func GetLanguageRuntime(annotations map[string]string) LanguageRuntime {
	if override, has := os.LookupEnv("ISTIO_LANG"); has {
		return fromString(override)
	}
	return fromString(annotations[LanguageRuntimeAnnotation])
}

func fromString(value string) LanguageRuntime {
	switch value {
	case "CEL":
		return CEL
	case "COMPAT":
		return COMPAT
	case "CEXL":
		return CEXL
	default:
		// TODO(kuat): temporary testing
		return COMPAT
	}
}

// NewBuilder returns an expression builder
func NewBuilder(finder ast.AttributeDescriptorFinder, mode LanguageRuntime) Compiler {
	switch mode {
	case CEL:
		return cel.NewBuilder(finder, cel.CEL)
	case COMPAT:
		return cel.NewBuilder(finder, cel.LegacySyntaxCEL)
	case CEXL:
		fallthrough
	default:
		return compiled.NewBuilder(finder)
	}
}

func (mode LanguageRuntime) String() string {
	switch mode {
	case CEL:
		return "CEL"
	case COMPAT:
		return "COMPAT"
	case CEXL:
		return "CEXL"
	default:
		return ""
	}
}
