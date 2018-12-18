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

package interpreter

import (
	"fmt"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter/functions"
)

// Dispatcher resolves function calls to their appropriate overload.
type Dispatcher interface {
	// Add one or more overloads, returning an error if any Overload has the
	// same Overload#Name.
	Add(overloads ...*functions.Overload) error

	// Dispatch a call to its appropriate Overload and return the result or
	// error.
	Dispatch(ctx *CallContext) ref.Value

	// FindOverload returns an Overload definition matching the provided
	// name.
	FindOverload(overload string) (*functions.Overload, bool)
}

// CallContext provides a description of a function call invocation.
type CallContext struct {
	call       *CallExpr
	args       []ref.Value
	activation Activation
	metadata   Metadata
}

// Function name to be invoked as it is written in an expression.
func (ctx *CallContext) Function() (string, string) {
	return ctx.call.Function, ctx.call.Overload
}

// Args to provide on the overload dispatch.
func (ctx *CallContext) Args() []ref.Value {
	return ctx.args
}

func (ctx *CallContext) String() string {
	return fmt.Sprintf("%s with %v", ctx.call.String(), ctx.args)
}

// NewDispatcher returns an empty Dispatcher.
//
// Typically this call would be used with functions#StandardOverloads:
//
//     dispatcher := NewDispatcher()
//     dispatcher.add(functions.StandardOverloads())
func NewDispatcher() Dispatcher {
	return &defaultDispatcher{
		overloads: make(map[string]*functions.Overload)}
}

// Helper types for tracking overloads by various dimensions.
type overloadMap map[string]*functions.Overload

type defaultDispatcher struct {
	overloads overloadMap
}

func (d *defaultDispatcher) Add(overloads ...*functions.Overload) error {
	for _, o := range overloads {
		// add the overload unless an overload of the same name has already
		// been provided before.
		if _, found := d.overloads[o.Operator]; found {
			return fmt.Errorf("overload already exists '%s'", o.Operator)
		}
		// Index the overload by function and by arg count.
		d.overloads[o.Operator] = o
	}
	return nil
}

func (d *defaultDispatcher) Dispatch(ctx *CallContext) ref.Value {
	function, overloadID := ctx.Function()
	operand := ctx.args[0]
	if overload, found := d.overloads[function]; found {
		if !operand.Type().HasTrait(overload.OperandTrait) {
			return types.NewErr("no such overload")
		}
		argCount := len(ctx.args)
		if argCount == 2 && overload.Binary != nil {
			return overload.Binary(ctx.args[0], ctx.args[1])
		}
		if argCount == 1 && overload.Unary != nil {
			return overload.Unary(ctx.args[0])
		}
		if overload.Function != nil {
			return overload.Function(ctx.args...)
		}
	}
	// Special dispatch for member functions.
	if operand.Type().HasTrait(traits.ReceiverType) {
		operand.(traits.Receiver).Receive(function, overloadID, ctx.args[1:])
	}
	return types.NewErr("no such overload")
}

func (d *defaultDispatcher) FindOverload(overload string) (*functions.Overload, bool) {
	o, found := d.overloads[overload]
	return o, found
}
