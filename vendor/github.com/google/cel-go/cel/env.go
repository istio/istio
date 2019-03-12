// Copyright 2019 Google LLC
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

package cel

import (
	"errors"
	"fmt"

	"github.com/google/cel-go/checker/decls"

	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/packages"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	"github.com/google/cel-go/parser"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Source interface representing a user-provided expression.
type Source interface {
	common.Source
}

// Ast interface representing the checked or unchecked expression, its source, and related metadata
// such as source position information.
type Ast interface {
	// Expr returns the proto serializable instance of the parsed/checked expression.
	Expr() *exprpb.Expr

	// IsChecked returns whether the Ast value has been successfully type-checked.
	IsChecked() bool

	// ResultType returns the output type of the expression if the Ast has been type-checked,
	// else returns decls.Dyn as the parse step cannot infer the type.
	ResultType() *exprpb.Type

	// Source returns a view of the input used to create the Ast. This source may be complete or
	// constructed from the SourceInfo.
	Source() Source

	// SourceInfo returns character offset and newling position information about expression
	// elements.
	SourceInfo() *exprpb.SourceInfo
}

// Env defines functions for parsing and type-checking expressions against a set of user-defined
// constants, variables, and functions. The Env interface also defines a method for generating
// evaluable programs from parsed and checked Asts.
type Env interface {
	// Check performs type-checking on the input Ast and yields a checked Ast and/or set of Issues.
	//
	// Checking has failed if the returned Issues value and its Issues.Err() value is non-nil.
	// Issues should be inspected if they are non-nil, but may not represent a fatal error.
	//
	// It is possible to have both non-nil Ast and Issues values returned from this call: however,
	// the mere presence of an Ast does not imply that it is valid for use.
	Check(ast Ast) (Ast, Issues)

	// Parse parses the input expression value `txt` to a Ast and/or a set of Issues.
	//
	// Parsing has failed if the returned Issues value and its Issues.Err() value is non-nil.
	// Issues should be inspected if they are non-nil, but may not represent a fatal error.
	//
	// It is possible to have both non-nil Ast and Issues values returned from this call; however,
	// the mere presence of an Ast does not imply that it is valid for use.
	Parse(txt string) (Ast, Issues)

	// Program generates an evaluable instance of the Ast within the environment (Env).
	Program(ast Ast, opts ...ProgramOption) (Program, error)
}

// Issues defines methods for inspecting the error details of parse and check calls.
//
// Note: in the future, non-fatal warnings and notices may be inspectable via the Issues interface.
type Issues interface {
	fmt.Stringer

	// Err returns an error value if the issues list contains one or more errors.
	Err() error

	// Errors returns the collection of errors encountered in more granular detail.
	Errors() []common.Error
}

// NewEnv creates an Env instance suitable for parsing and checking expressions against a set of
// user-defined constants, variables, and functions. Macros and the standard built-ins are enabled
// by default.
//
// See the EnvOptions for the options that can be used to configure the environment.
func NewEnv(opts ...EnvOption) (Env, error) {
	e := &env{
		declarations:   checker.StandardDeclarations(),
		enableBuiltins: true,
		macros:         parser.AllMacros,
		pkg:            packages.DefaultPackage,
		types:          types.NewProvider(),
	}
	// Customized the environment using the provided EnvOption values. If an error is
	// generated at any step this, will be returned as a nil Env with a non-nil error.
	var err error
	for _, opt := range opts {
		e, err = opt(e)
		if err != nil {
			return nil, err
		}
	}
	return e, nil
}

// astValue is the internal implementation of the ast interface.
type astValue struct {
	expr    *exprpb.Expr
	info    *exprpb.SourceInfo
	source  Source
	refMap  map[int64]*exprpb.Reference
	typeMap map[int64]*exprpb.Type
}

// Expr implements the Ast interface method.
func (ast *astValue) Expr() *exprpb.Expr {
	return ast.expr
}

// IsChecked implements the Ast interface method.
func (ast *astValue) IsChecked() bool {
	return ast.refMap != nil && ast.typeMap != nil
}

// SourceInfo implements the Ast interface method.
func (ast *astValue) SourceInfo() *exprpb.SourceInfo {
	return ast.info
}

// ResultType implements the Ast interface method.
func (ast *astValue) ResultType() *exprpb.Type {
	if !ast.IsChecked() {
		return decls.Dyn
	}
	return ast.typeMap[ast.expr.Id]
}

// Source implements the Ast interface method.
func (ast *astValue) Source() Source {
	return ast.source
}

// env is the internal implementation of the Env interface.
type env struct {
	declarations   []*exprpb.Decl
	enableBuiltins bool
	macros         []parser.Macro
	pkg            packages.Packager
	types          ref.TypeProvider
}

// Check implements the Env interface method.
func (e *env) Check(ast Ast) (Ast, Issues) {
	ce := checker.NewEnv(e.pkg, e.types)
	ce.Add(e.declarations...)
	pe, err := AstToParsedExpr(ast)
	if err != nil {
		errs := common.NewErrors(ast.Source())
		errs.ReportError(common.NoLocation, err.Error())
		return nil, &issues{errs: errs}
	}
	res, errs := checker.Check(pe, ast.Source(), ce)
	if len(errs.GetErrors()) > 0 {
		return nil, &issues{errs: errs}
	}
	// Manually create the Ast to ensure that the Ast source information (which may be more
	// detailed than the information provided by Check), is returned to the caller.
	return &astValue{
		source:  ast.Source(),
		expr:    res.GetExpr(),
		info:    res.GetSourceInfo(),
		refMap:  res.GetReferenceMap(),
		typeMap: res.GetTypeMap()}, nil
}

// Parse implements the Env interface method.
func (e *env) Parse(txt string) (Ast, Issues) {
	src := common.NewTextSource(txt)
	res, errs := parser.ParseWithMacros(src, e.macros)
	if len(errs.GetErrors()) > 0 {
		return nil, &issues{errs: errs}
	}
	// Manually create the Ast to ensure that the text source information is propagated on
	// subsequent calls to Check.
	return &astValue{
		source: Source(src),
		expr:   res.GetExpr(),
		info:   res.GetSourceInfo()}, nil
}

// Program implements the Env interface method.
func (e *env) Program(ast Ast, opts ...ProgramOption) (Program, error) {
	if e.enableBuiltins {
		opts = append(
			[]ProgramOption{Functions(functions.StandardOverloads()...)},
			opts...)
	}
	return newProgram(e, ast, opts...)
}

// issues is the internal implementation of the Issues interface.
type issues struct {
	errs *common.Errors
}

// Err implements the Issues interface method.
func (i *issues) Err() error {
	if len(i.errs.GetErrors()) > 0 {
		return errors.New(i.errs.ToDisplayString())
	}
	return nil
}

// Errors implements the Issues interface method.
func (i *issues) Errors() []common.Error {
	return i.errs.GetErrors()
}

// String converts the issues to a suitable display string.
func (i *issues) String() string {
	return i.errs.ToDisplayString()
}
