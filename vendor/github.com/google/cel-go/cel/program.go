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
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Program is an evaluable view of an Ast.
type Program interface {
	// Eval returns the result of an evaluation of the Ast and environment against the input vars.
	//
	// If the evaluation is an error, the result will be nil with a non-nil error.
	//
	// If the OptTrackState or OptExhaustiveEval is used, the EvalDetails response will be non-nil.
	Eval(vars interpreter.Activation) (ref.Val, EvalDetails, error)
}

// EvalDetails holds additional information observed during the Eval() call.
type EvalDetails interface {
	// State of the evaluation, non-nil if the OptTrackState or OptExhaustiveEval is specified
	// within EvalOptions.
	State() interpreter.EvalState
}

// Vars takes an input map of variables and returns an Activation.
func Vars(vars map[string]interface{}) interpreter.Activation {
	return interpreter.NewActivation(vars)
}

// NoVars returns an empty Activation.
func NoVars() interpreter.Activation {
	return interpreter.NewActivation(map[string]interface{}{})
}

// evalDetails is the internal implementation of the EvalDetails interface.
type evalDetails struct {
	state interpreter.EvalState
}

// State implements the Result interface method.
func (ed *evalDetails) State() interpreter.EvalState {
	return ed.state
}

// prog is the internal implementation of the Program interface.
type prog struct {
	*env
	evalOpts      EvalOption
	defaultVars   interpreter.Activation
	dispatcher    interpreter.Dispatcher
	interpreter   interpreter.Interpreter
	interpretable interpreter.Interpretable
}

// progFactory is a helper alias for marking a program creation factory function.
type progFactory func(interpreter.EvalState) (Program, error)

// progGen holds a reference to a progFactory instance and implements the Program interface.
type progGen struct {
	factory progFactory
}

// newProgram creates a program instance with an environment, an ast, and an optional list of
// ProgramOption values.
//
// If the program cannot be configured the prog will be nil, with a non-nil error response.
func newProgram(e *env, ast Ast, opts ...ProgramOption) (Program, error) {
	// Build the dispatcher, interpreter, and default program value.
	disp := interpreter.NewDispatcher()
	interp := interpreter.NewInterpreter(disp, e.pkg, e.types)
	p := &prog{
		env:         e,
		dispatcher:  disp,
		interpreter: interp}

	// Configure the program via the ProgramOption values.
	var err error
	for _, opt := range opts {
		p, err = opt(p)
		if err != nil {
			return nil, err
		}
	}

	// Translate the EvalOption flags into InterpretableDecorator instances.
	decorators := []interpreter.InterpretableDecorator{}
	// Enable constant folding first.
	if p.evalOpts&OptFoldConstants == OptFoldConstants {
		decorators = append(decorators, interpreter.FoldConstants())
	}
	// Enable exhaustive eval over state tracking since it offers a superset of features.
	if p.evalOpts&OptExhaustiveEval == OptExhaustiveEval {
		// State tracking requires that each Eval() call operate on an isolated EvalState
		// object; hence, the presence of the factory.
		factory := func(state interpreter.EvalState) (Program, error) {
			decs := append(decorators, interpreter.ExhaustiveEval(state))
			clone := &prog{
				evalOpts:    p.evalOpts,
				defaultVars: p.defaultVars,
				env:         e,
				dispatcher:  disp,
				interpreter: interp}
			return initInterpretable(clone, ast, decs)
		}
		return &progGen{factory: factory}, nil
	}
	// Enable state tracking last since it too requires the factory approach but is less
	// featured than the ExhaustiveEval decorator.
	if p.evalOpts&OptTrackState == OptTrackState {
		factory := func(state interpreter.EvalState) (Program, error) {
			decs := append(decorators, interpreter.TrackState(state))
			clone := &prog{
				evalOpts:    p.evalOpts,
				defaultVars: p.defaultVars,
				env:         e,
				dispatcher:  disp,
				interpreter: interp}
			return initInterpretable(clone, ast, decs)
		}
		return &progGen{factory: factory}, nil
	}
	return initInterpretable(p, ast, decorators)
}

// initIterpretable creates a checked or unchecked interpretable depending on whether the Ast
// has been run through the type-checker.
func initInterpretable(
	p *prog,
	ast Ast,
	decorators []interpreter.InterpretableDecorator) (Program, error) {
	var err error
	// Unchecked programs do not contain type and reference information and may be
	// slower to execute than their checked counterparts.
	if !ast.IsChecked() {
		p.interpretable, err =
			p.interpreter.NewUncheckedInterpretable(ast.Expr(), decorators...)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	// When the AST has been checked it contains metadata that can be used to speed up program
	// execution.
	var checked *exprpb.CheckedExpr
	checked, err = AstToCheckedExpr(ast)
	if err != nil {
		return nil, err
	}
	p.interpretable, err = p.interpreter.NewInterpretable(checked, decorators...)
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Eval implements the Program interface method.
func (p *prog) Eval(vars interpreter.Activation) (ref.Val, EvalDetails, error) {
	// Build a hierarchical activation if there are default vars set.
	if p.defaultVars != nil {
		vars = interpreter.NewHierarchicalActivation(p.defaultVars, vars)
	}
	v := p.interpretable.Eval(vars)
	// The output of an internal Eval may have a value (`v`) that is a types.Err. This step
	// translates the CEL value to a Go error response. This interface does not quite match the
	// RPC signature which allows for multiple errors to be returned, but should be sufficient.
	if types.IsError(v) {
		return nil, nil, v.Value().(error)
	}
	return v, nil, nil
}

// Eval implements the Program interface method.
func (gen *progGen) Eval(vars interpreter.Activation) (ref.Val, EvalDetails, error) {
	// The factory based Eval() differs from the standard evaluation model in that it generates a
	// new EvalState instance for each call to ensure that unique evaluations yield unique stateful
	// results.
	state := interpreter.NewEvalState()

	// Generate a new instance of the interpretable using the factory configured during the call to
	// newProgram().
	p, err := gen.factory(state)
	if err != nil {
		return nil, nil, err
	}

	// Evaluate the input, returning the result and the 'state' as EvalDetails.
	v, _, err := p.Eval(vars)
	if err != nil {
		return nil, nil, err
	}
	return v, &evalDetails{state: state}, nil
}
