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

	"github.com/google/cel-go/interpreter/functions"
)

// Dispatcher resolves function calls to their appropriate overload.
type Dispatcher interface {
	// Add one or more overloads, returning an error if any Overload has the
	// same Overload#Name.
	Add(overloads ...*functions.Overload) error

	// FindOverload returns an Overload definition matching the provided
	// name.
	FindOverload(overload string) (*functions.Overload, bool)
}

// NewDispatcher returns an empty Dispatcher instance.
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

// FindOverload implements the Dispatcher.FindOverload interface method.
func (d *defaultDispatcher) FindOverload(overload string) (*functions.Overload, bool) {
	o, found := d.overloads[overload]
	return o, found
}
