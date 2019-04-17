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
	"sync"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Activation used to resolve identifiers by name and references by id.
//
// An Activation is the primary mechanism by which a caller supplies input into a CEL program.
type Activation interface {
	// ResolveName returns a value from the activation by qualified name, or false if the name
	// could not be found.
	ResolveName(name string) (ref.Val, bool)

	// Parent returns the parent of the current activation, may be nil.
	// If non-nil, the parent will be searched during resolve calls.
	Parent() Activation
}

// EmptyActivation returns a variable free activation.
func EmptyActivation() Activation {
	// This call cannot fail.
	a, _ := NewActivation(map[string]interface{}{})
	return a
}

// NewActivation returns an activation based on a map-based binding where the map keys are
// expected to be qualified names used with ResolveName calls.
//
// The input `bindings` may either be of type `Activation` or `map[string]interface{}`.
//
// When the bindings are a `map` form whose values are not of `ref.Val` type, the values will be
// converted to CEL values (if possible) using the `types.DefaultTypeAdapter`.
func NewActivation(bindings interface{}) (Activation, error) {
	return NewAdaptingActivation(types.DefaultTypeAdapter, bindings)
}

// NewAdaptingActivation returns an actvation which is capable of adapting `bindings` from native
// Go values to equivalent CEL `ref.Val` objects.
//
// The input `bindings` may either be of type `Activation` or `map[string]interface{}`.
//
// When the bindings are a `map` the values may be one of the following types:
//   - `ref.Val`: a CEL value instance.
//   - `func() ref.Val`: a CEL value supplier.
//   - other: a native value which must be converted to a CEL `ref.Val` by the `adapter`.
func NewAdaptingActivation(adapter ref.TypeAdapter, bindings interface{}) (Activation, error) {
	a, isActivation := bindings.(Activation)
	if isActivation {
		return a, nil
	}
	m, isMap := bindings.(map[string]interface{})
	if !isMap {
		return nil, fmt.Errorf(
			"activation input must be an activation or map[string]interface: got %T",
			bindings)
	}
	var allRefVals = true
	for _, v := range m {
		_, isVal := v.(ref.Val)
		if !isVal {
			allRefVals = false
			break
		}
	}
	if allRefVals {
		return &mapActivation{bindings: m}, nil
	}
	return &mapActivation{adapter: adapter, bindings: m}, nil
}

// mapActivation which implements Activation and maps of named values.
//
// Named bindings may lazily supply values by providing a function which accepts no arguments and
// produces an interface value.
type mapActivation struct {
	adapter  ref.TypeAdapter
	bindings map[string]interface{}
}

// Parent implements the Activation interface method.
func (a *mapActivation) Parent() Activation {
	return nil
}

// ResolveName implements the Activation interface method.
func (a *mapActivation) ResolveName(name string) (ref.Val, bool) {
	if object, found := a.bindings[name]; found {
		switch object.(type) {
		// Resolve a lazily bound value.
		case func() ref.Val:
			val := object.(func() ref.Val)()
			a.bindings[name] = val
			return val, true
		// Otherwise, return the bound value.
		case ref.Val:
			return object.(ref.Val), true
		default:
			if a.adapter != nil {
				return a.adapter.NativeToValue(object), true
			}
		}
	}
	return nil, false
}

// hierarchicalActivation which implements Activation and contains a parent and
// child activation.
type hierarchicalActivation struct {
	parent Activation
	child  Activation
}

// Parent implements the Activation interface method.
func (a *hierarchicalActivation) Parent() Activation {
	return a.parent
}

// ResolveName implements the Activation interface method.
func (a *hierarchicalActivation) ResolveName(name string) (ref.Val, bool) {
	if object, found := a.child.ResolveName(name); found {
		return object, found
	}
	return a.parent.ResolveName(name)
}

// NewHierarchicalActivation takes two activations and produces a new one which prioritizes
// resolution in the child first and parent(s) second.
func NewHierarchicalActivation(parent Activation, child Activation) Activation {
	return &hierarchicalActivation{parent, child}
}

// newVarActivation returns a new varActivation instance.
func newVarActivation(parent Activation, name string) *varActivation {
	return &varActivation{
		parent: parent,
		name:   name,
	}
}

// varActivation represents a single mutable variable binding.
//
// This activation type should only be used within folds as the fold loop controls the object
// life-cycle.
type varActivation struct {
	parent Activation
	name   string
	val    ref.Val
}

// Parent implements the Activation interface method.
func (v *varActivation) Parent() Activation {
	return v.parent
}

// ResolveName implements the Activation interface method.
func (v *varActivation) ResolveName(name string) (ref.Val, bool) {
	if name == v.name {
		return v.val, true
	}
	return v.parent.ResolveName(name)
}

var (
	// pool of var activations to reduce allocations during folds.
	varActivationPool = &sync.Pool{
		New: func() interface{} {
			return &varActivation{}
		},
	}
)
