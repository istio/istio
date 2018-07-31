// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

type (
	// FunctionalBuiltin1 defines an interface for simple functional built-ins.
	//
	// Implement this interface if your built-in function takes one input and
	// produces one output.
	//
	// If an error occurs, the functional built-in should return a descriptive
	// message. The message should not be prefixed with the built-in name as the
	// framework takes care of this.
	FunctionalBuiltin1 func(op1 ast.Value) (output ast.Value, err error)

	// FunctionalBuiltin2 defines an interface for simple functional built-ins.
	//
	// Implement this interface if your built-in function takes two inputs and
	// produces one output.
	//
	// If an error occurs, the functional built-in should return a descriptive
	// message. The message should not be prefixed with the built-in name as the
	// framework takes care of this.
	FunctionalBuiltin2 func(op1, op2 ast.Value) (output ast.Value, err error)

	// FunctionalBuiltin3 defines an interface for simple functional built-ins.
	//
	// Implement this interface if your built-in function takes three inputs and
	// produces one output.
	//
	// If an error occurs, the functional built-in should return a descriptive
	// message. The message should not be prefixed with the built-in name as the
	// framework takes care of this.
	FunctionalBuiltin3 func(op1, op2, op3 ast.Value) (output ast.Value, err error)

	// BuiltinContext contains context from the evaluator that may be used by
	// built-in functions.
	BuiltinContext struct {
		Cache    builtins.Cache
		Location *ast.Location
		Tracer   Tracer
		QueryID  uint64
		ParentID uint64
	}

	// BuiltinFunc defines an interface for implementing built-in functions.
	// The built-in function is called with the plugged operands from the call
	// (including the output operands.) The implementation should evaluate the
	// operands and invoke the iteraror for each successful/defined output
	// value.
	BuiltinFunc func(bctx BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error
)

// RegisterBuiltinFunc adds a new built-in function to the evaluation engine.
func RegisterBuiltinFunc(name string, f BuiltinFunc) {
	builtinFunctions[name] = f
}

// RegisterFunctionalBuiltin1 adds a new built-in function to the evaluation
// engine.
func RegisterFunctionalBuiltin1(name string, fun FunctionalBuiltin1) {
	builtinFunctions[name] = functionalWrapper1(name, fun)
}

// RegisterFunctionalBuiltin2 adds a new built-in function to the evaluation
// engine.
func RegisterFunctionalBuiltin2(name string, fun FunctionalBuiltin2) {
	builtinFunctions[name] = functionalWrapper2(name, fun)
}

// RegisterFunctionalBuiltin3 adds a new built-in function to the evaluation
// engine.
func RegisterFunctionalBuiltin3(name string, fun FunctionalBuiltin3) {
	builtinFunctions[name] = functionalWrapper3(name, fun)
}

// BuiltinEmpty is used to signal that the built-in function evaluated, but the
// result is undefined so evaluation should not continue.
type BuiltinEmpty struct{}

func (BuiltinEmpty) Error() string {
	return "<empty>"
}

var builtinFunctions = map[string]BuiltinFunc{}

func functionalWrapper1(name string, fn FunctionalBuiltin1) BuiltinFunc {
	return func(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
		result, err := fn(args[0].Value)
		if err == nil {
			return iter(ast.NewTerm(result))
		}
		if _, empty := err.(BuiltinEmpty); empty {
			return nil
		}
		return handleBuiltinErr(name, bctx.Location, err)
	}
}

func functionalWrapper2(name string, fn FunctionalBuiltin2) BuiltinFunc {
	return func(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
		result, err := fn(args[0].Value, args[1].Value)
		if err == nil {
			return iter(ast.NewTerm(result))
		}
		if _, empty := err.(BuiltinEmpty); empty {
			return nil
		}
		return handleBuiltinErr(name, bctx.Location, err)
	}
}

func functionalWrapper3(name string, fn FunctionalBuiltin3) BuiltinFunc {
	return func(bctx BuiltinContext, args []*ast.Term, iter func(*ast.Term) error) error {
		result, err := fn(args[0].Value, args[1].Value, args[2].Value)
		if err == nil {
			return iter(ast.NewTerm(result))
		}
		if _, empty := err.(BuiltinEmpty); empty {
			return nil
		}
		return handleBuiltinErr(name, bctx.Location, err)
	}
}

func handleBuiltinErr(name string, loc *ast.Location, err error) error {
	switch err := err.(type) {
	case BuiltinEmpty:
		return nil
	case builtins.ErrOperand:
		return &Error{
			Code:     TypeErr,
			Message:  fmt.Sprintf("%v: %v", string(name), err.Error()),
			Location: loc,
		}
	default:
		return &Error{
			Code:     InternalErr,
			Message:  fmt.Sprintf("%v: %v", string(name), err.Error()),
			Location: loc,
		}
	}
}
