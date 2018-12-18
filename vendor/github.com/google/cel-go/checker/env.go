// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package checker

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/packages"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/pb"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/parser"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Env is the environment for type checking.
// It consists of a Packager, a Type Provider, declarations, and collection of errors encountered
// during checking.
type Env struct {
	packager     packages.Packager
	typeProvider ref.TypeProvider

	declarations *decls.Scopes
}

// NewEnv returns a new *Env with the given parameters.
func NewEnv(packager packages.Packager,
	typeProvider ref.TypeProvider) *Env {
	declarations := decls.NewScopes()
	declarations.Push()

	return &Env{
		packager:     packager,
		typeProvider: typeProvider,
		declarations: declarations,
	}
}

// NewStandardEnv returns a new *Env with the given params plus standard declarations.
func NewStandardEnv(packager packages.Packager,
	typeProvider ref.TypeProvider) *Env {
	e := NewEnv(packager, typeProvider)
	if err := e.Add(StandardDeclarations()...); err != nil {
		// The standard declaration set should never have duplicate declarations.
		panic(err)
	}
	return e
}

// Add adds new Decl protos to the Env.
// Returns an error for identifier redeclarations.
func (e *Env) Add(decls ...*exprpb.Decl) error {
	errMsgs := make([]errorMsg, 0)
	for _, decl := range decls {
		switch decl.DeclKind.(type) {
		case *exprpb.Decl_Ident:
			errMsgs = append(errMsgs, e.addIdent(sanitizeIdent(decl)))
		case *exprpb.Decl_Function:
			errMsgs = append(errMsgs, e.addFunction(sanitizeFunction(decl))...)
		}
	}
	return formatError(errMsgs)
}

// LookupIdent returns a Decl proto for typeName as an identifier in the Env.
// Returns nil if no such identifier is found in the Env.
func (e *Env) LookupIdent(typeName string) *exprpb.Decl {
	for _, candidate := range e.packager.ResolveCandidateNames(typeName) {
		if ident := e.declarations.FindIdent(candidate); ident != nil {
			return ident
		}

		// Next try to import the name as a reference to a message type. If found,
		// the declaration is added to the outest (global) scope of the
		// environment, so next time we can access it faster.
		if t, found := e.typeProvider.FindType(candidate); found {
			decl := decls.NewIdent(candidate, t, nil)
			e.declarations.AddIdent(decl)
			return decl
		}

		// Next try to import this as an enum value by splitting the name in a type prefix and
		// the enum inside.
		if enumValue := e.typeProvider.EnumValue(candidate); enumValue.Type() != types.ErrType {
			decl := decls.NewIdent(candidate,
				decls.Int,
				&exprpb.Constant{
					ConstantKind: &exprpb.Constant_Int64Value{
						Int64Value: int64(enumValue.(types.Int))}})
			e.declarations.AddIdent(decl)
			return decl
		}
	}
	return nil
}

// LookupFunction returns a Decl proto for typeName as a function in env.
// Returns nil if no such function is found in env.
func (e *Env) LookupFunction(typeName string) *exprpb.Decl {
	for _, candidate := range e.packager.ResolveCandidateNames(typeName) {
		if fn := e.declarations.FindFunction(candidate); fn != nil {
			return fn
		}
	}
	return nil
}

// addOverload adds overload to function declaration f.
// Returns one or more errorMsg values if the overload overlaps with an existing overload or macro.
func (e *Env) addOverload(f *exprpb.Decl, overload *exprpb.Decl_FunctionDecl_Overload) []errorMsg {
	errMsgs := make([]errorMsg, 0)
	function := f.GetFunction()
	emptyMappings := newMapping()
	overloadFunction := decls.NewFunctionType(overload.GetResultType(),
		overload.GetParams()...)
	overloadErased := substitute(emptyMappings, overloadFunction, true)
	for _, existing := range function.GetOverloads() {
		existingFunction := decls.NewFunctionType(existing.GetResultType(),
			existing.GetParams()...)
		existingErased := substitute(emptyMappings, existingFunction, true)
		overlap := isAssignable(emptyMappings, overloadErased, existingErased) != nil ||
			isAssignable(emptyMappings, existingErased, overloadErased) != nil
		if overlap &&
			overload.GetIsInstanceFunction() == existing.GetIsInstanceFunction() {
			errMsgs = append(errMsgs,
				overlappingOverloadError(f.Name,
					overload.GetOverloadId(), overloadFunction,
					existing.GetOverloadId(), existingFunction))
		}
	}

	for _, macro := range parser.AllMacros {
		if macro.GetName() == f.Name && macro.GetIsInstanceStyle() == overload.GetIsInstanceFunction() &&
			macro.GetArgCount() == len(overload.GetParams()) {
			errMsgs = append(errMsgs, overlappingMacroError(f.Name, macro.GetArgCount()))
		}
	}
	if len(errMsgs) > 0 {
		return errMsgs
	}
	function.Overloads = append(function.GetOverloads(), overload)
	return errMsgs
}

// addFunction adds the function Decl to the Env.
// Adds a function decl if one doesn't already exist, then adds all overloads from the Decl.
// If overload overlaps with an existing overload, adds to the errors  in the Env instead.
func (e *Env) addFunction(decl *exprpb.Decl) []errorMsg {
	current := e.declarations.FindFunction(decl.Name)
	if current == nil {
		//Add the function declaration without overloads and check the overloads below.
		current = decls.NewFunction(decl.Name)
		e.declarations.AddFunction(current)
	}

	errorMsgs := make([]errorMsg, 0)
	for _, overload := range decl.GetFunction().GetOverloads() {
		errorMsgs = append(errorMsgs, e.addOverload(current, overload)...)
	}
	return errorMsgs
}

// addIdent adds the Decl to the declarations in the Env.
// Returns a non-empty errorMsg if the identifier is already declared in the scope.
func (e *Env) addIdent(decl *exprpb.Decl) errorMsg {
	current := e.declarations.FindIdentInScope(decl.Name)
	if current != nil {
		return overlappingIdentifierError(decl.Name)
	}
	e.declarations.AddIdent(decl)
	return ""
}

// sanitizeFunction replaces well-known types referenced by message name with their equivalent
// CEL built-in type instances.
func sanitizeFunction(decl *exprpb.Decl) *exprpb.Decl {
	fn := decl.GetFunction()
	// Determine whether the declaration requires replacements from proto-based message type
	// references to well-known CEL type references.
	var needsSanitizing bool
	for _, o := range fn.GetOverloads() {
		if isObjectWellKnownType(o.GetResultType()) {
			needsSanitizing = true
			break
		}
		for _, p := range o.GetParams() {
			if isObjectWellKnownType(p) {
				needsSanitizing = true
				break
			}
		}
	}

	// Early return if the declaration requires no modification.
	if !needsSanitizing {
		return decl
	}

	// Sanitize all of the overloads if any overload requires an update to its type references.
	overloads := make([]*exprpb.Decl_FunctionDecl_Overload, 0, len(fn.GetOverloads()))
	for i, o := range fn.GetOverloads() {
		var sanitized bool
		rt := o.GetResultType()
		if isObjectWellKnownType(rt) {
			rt = getObjectWellKnownType(rt)
			sanitized = true
		}
		params := make([]*exprpb.Type, 0, len(o.GetParams()))
		copy(params, o.GetParams())
		for j, p := range params {
			if isObjectWellKnownType(p) {
				params[j] = getObjectWellKnownType(p)
				sanitized = true
			}
		}
		// If sanitized, replace the overload definition.
		if sanitized {
			if o.IsInstanceFunction {
				overloads[i] =
					decls.NewInstanceOverload(o.GetOverloadId(), params, rt)
			} else {
				overloads[i] =
					decls.NewOverload(o.GetOverloadId(), params, rt)
			}
		} else {
			// Otherwise, preserve the original overload.
			overloads[i] = o
		}
	}
	return decls.NewFunction(decl.GetName(), overloads...)
}

// sanitizeIdent replaces the identifier's well-known types referenced by message name with
// references to CEL built-in type instances.
func sanitizeIdent(decl *exprpb.Decl) *exprpb.Decl {
	id := decl.GetIdent()
	t := id.GetType()
	if !isObjectWellKnownType(t) {
		return decl
	}
	return decls.NewIdent(decl.GetName(), getObjectWellKnownType(t), id.GetValue())
}

// isObjectWellKnownType returns true if the input type is an OBJECT type with a message name
// that corresponds the message name of a built-in CEL type.
func isObjectWellKnownType(t *exprpb.Type) bool {
	if kindOf(t) != kindObject {
		return false
	}
	_, found := pb.CheckedWellKnowns[t.GetMessageType()]
	return found
}

// getObjectWellKnownType returns the built-in CEL type declaration for input type's message name.
func getObjectWellKnownType(t *exprpb.Type) *exprpb.Type {
	return pb.CheckedWellKnowns[t.GetMessageType()]
}

// enterScope pushes a new declaration set onto the stack, to ensure variables
// and function identifiers are appropriately shadowed / enclosed as needed.
func (e *Env) enterScope() {
	e.declarations.Push()
}

// exitScope pops the local declarations of the current frame.
func (e *Env) exitScope() {
	e.declarations.Pop()
}

// errorMsg is a type alias meant to represent error-based return values which
// may be accumulated into an error at a later point in execution.
type errorMsg string

func overlappingIdentifierError(name string) errorMsg {
	return errorMsg(fmt.Sprintf("overlapping identifier for name '%s'", name))
}

func overlappingOverloadError(name string,
	overloadID1 string, f1 *exprpb.Type,
	overloadID2 string, f2 *exprpb.Type) errorMsg {
	return errorMsg(fmt.Sprintf(
		"overlapping overload for name '%s' (type '%s' with overloadId: '%s' "+
			"cannot be distinguished from '%s' with overloadId: '%s')",
		name,
		FormatCheckedType(f1),
		overloadID1,
		FormatCheckedType(f2),
		overloadID2))
}

func overlappingMacroError(name string, argCount int) errorMsg {
	return errorMsg(fmt.Sprintf(
		"overlapping macro for name '%s' with %d args", name, argCount))
}

func formatError(errMsgs []errorMsg) error {
	errStrs := make([]string, 0)
	if len(errMsgs) > 0 {
		for i := 0; i < len(errMsgs); i++ {
			if errMsgs[i] != "" {
				errStrs = append(errStrs, string(errMsgs[i]))
			}
		}
	}
	if len(errStrs) > 0 {
		return fmt.Errorf("%s", strings.Join(errStrs, "\n"))
	}
	return nil
}
