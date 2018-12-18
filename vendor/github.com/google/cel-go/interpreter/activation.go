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
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Activation used to resolve identifiers by name and references by id.
//
// An Activation is the primary mechanism by which a caller supplies input
// into a CEL program.
type Activation interface {

	// ResolveReference returns a value from the activation by expression id,
	// or false if the id-based reference could not be found.
	ResolveReference(exprID int64) (ref.Value, bool)

	// ResolveName returns a value from the activation by qualified name, or
	// false if the name could not be found.
	ResolveName(name string) (ref.Value, bool)

	// Parent returns the parent of the current activation, may be nil.
	// If non-nil, the parent will be searched during resolve calls.
	Parent() Activation
}

// NewActivation returns an activation based on a map-based binding where the
// map keys are expected to be qualified names used with ResolveName calls.
// TODO: supply references from checkedpb.proto.
func NewActivation(bindings map[string]interface{}) Activation {
	return &mapActivation{bindings: bindings}
}

// mapActivation which implements Activation and maps of named and referenced
// values.
//
// Named bindings may lazily supply values by providing a function which
// accepts no arguments and produces an interface value.
// TODO: consider passing the current activation to the supplier.
type mapActivation struct {
	references map[int64]ref.Value
	bindings   map[string]interface{}
}

func (a *mapActivation) Parent() Activation {
	return nil
}

func (a *mapActivation) ResolveName(name string) (ref.Value, bool) {
	if object, found := a.bindings[name]; found {
		switch object.(type) {
		// Resolve a lazily bound value.
		case func() ref.Value:
			return object.(func() ref.Value)(), true
		// Otherwise, return the bound value.
		case ref.Value:
			return object.(ref.Value), true
		default:
			return types.NativeToValue(object), true
		}
	}
	return nil, false
}

func (a *mapActivation) ResolveReference(exprID int64) (ref.Value, bool) {
	object, found := a.references[exprID]
	return object, found
}

// hierarchicalActivation which implements Activation and contains a parent and
// child activation.
type hierarchicalActivation struct {
	parent Activation
	child  Activation
}

func (a *hierarchicalActivation) Parent() Activation {
	return a.parent
}

func (a *hierarchicalActivation) ResolveName(name string) (ref.Value, bool) {
	if object, found := a.child.ResolveName(name); found {
		return object, found
	}
	return a.parent.ResolveName(name)
}

func (a *hierarchicalActivation) ResolveReference(exprID int64) (ref.Value, bool) {
	if object, found := a.child.ResolveReference(exprID); found {
		return object, found
	}
	return a.parent.ResolveReference(exprID)
}

// NewHierarchicalActivation takes two activations and produces a new one which prioritizes
// resolution in the child first and parent(s) second.
func NewHierarchicalActivation(parent Activation, child Activation) Activation {
	return &hierarchicalActivation{parent, child}
}
