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

	// Dispatch a call to its appropriate Overload and set the evaluation
	// state for the call to the return value.
	Dispatch(call *CallExpr)

	// FindOverload returns an Overload definition matching the provided
	// name.
	FindOverload(overload string) (*functions.Overload, bool)

	// Init clones the configuration of the current Dispatcher and returns
	// a new one with its own MutableEvalState sharing the current Dispatcher's
	// overload map.
	Init(state MutableEvalState) Dispatcher
}

// NewDispatcher returns an empty Dispatcher instance.
//
// Functions may be added to the Dispatcher via the Add() call. At the
// creation of a new Interpretable, the Dispatcher is cloned and given an
// instance of a MutableEvalState for the purpose of gathering call args
// and recording call responses in-place.
func NewDispatcher() Dispatcher {
	return &defaultDispatcher{
		overloads: make(map[string]*functions.Overload)}
}

// overloadMap helper type for indexing overloads by function name.
type overloadMap map[string]*functions.Overload

// defaultDispatcher struct which contains an overload map and a state
// instance used to track call args and return values.
type defaultDispatcher struct {
	overloads overloadMap
	state     MutableEvalState
}

// Init implements the Dispatcher.Init interface method.
func (d *defaultDispatcher) Init(state MutableEvalState) Dispatcher {
	return &defaultDispatcher{
		overloads: d.overloads,
		state:     state,
	}
}

// Add implements the Dispatcher.Add interface method.
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

// Dispatcher implements the Dispatcher.Dispatch interface method.
func (d *defaultDispatcher) Dispatch(call *CallExpr) {
	s := d.state
	function := call.Function
	argCount := len(call.Args)
	if overload, found := d.overloads[function]; found {
		if argCount == 0 {
			s.SetValue(call.ID, overload.Function())
			return
		}
		arg0, _ := s.Value(call.Args[0])
		if !arg0.Type().HasTrait(overload.OperandTrait) {
			s.SetValue(call.ID, types.NewErr("no such overload"))
			return
		}
		switch argCount {
		case 1:
			s.SetValue(call.ID, overload.Unary(arg0))
			return
		case 2:
			arg1, _ := s.Value(call.Args[1])
			s.SetValue(call.ID, overload.Binary(arg0, arg1))
			return
		case 3:
			arg1, _ := s.Value(call.Args[1])
			arg2, _ := s.Value(call.Args[2])
			s.SetValue(call.ID, overload.Function(arg0, arg1, arg2))
			return
		default:
			args := make([]ref.Value, argCount, argCount)
			for i, argID := range call.Args {
				val, _ := s.Value(argID)
				args[i] = val
			}
			s.SetValue(call.ID, overload.Function(args...))
			return
		}
	}
	// Special dispatch for type-specific extension functions.
	if argCount == 0 {
		// If we're here, then there wasn't a zero-arg global function,
		// and there's definitely no member function without an operand.
		s.SetValue(call.ID, types.NewErr("no such overload"))
		return
	}
	arg0, _ := s.Value(call.Args[0])
	if arg0.Type().HasTrait(traits.ReceiverType) {
		overload := call.Overload
		args := make([]ref.Value, argCount-1, argCount-1)
		for i, argID := range call.Args {
			if i == 0 {
				continue
			}
			val, _ := s.Value(argID)
			args[i] = val
		}
		s.SetValue(call.ID, arg0.(traits.Receiver).Receive(function, overload, args))
		return
	}
	s.SetValue(call.ID, types.NewErr("no such overload"))
}

// FindOverload implements the Dispatcher.FindOverload interface method.
func (d *defaultDispatcher) FindOverload(overload string) (*functions.Overload, bool) {
	o, found := d.overloads[overload]
	return o, found
}
