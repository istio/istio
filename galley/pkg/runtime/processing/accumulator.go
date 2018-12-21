//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import "istio.io/istio/galley/pkg/runtime/resource"

// TransformFn transforms an entry to a custom type, before storing it in a collection.
type TransformFn func(entry resource.Entry) (interface{}, error)

// Accumulator handles incoming events, optionally applies a conversion function, before setting the values
// in a backing collection
type Accumulator struct {
	xform      TransformFn
	collection *Collection
}

var _ Handler = &Accumulator{}

// NewAccumulator returns a new Accumulator
func NewAccumulator(c *Collection, xform TransformFn) *Accumulator {
	if xform == nil {
		xform = func(entry resource.Entry) (interface{}, error) { return entry, nil }
	}

	return &Accumulator{
		xform:      xform,
		collection: c,
	}
}

// Handle implements Handler
func (a *Accumulator) Handle(ev resource.Event) bool {
	switch ev.Kind {
	case resource.Added, resource.Updated:
		i, err := a.xform(ev.Entry)
		if err != nil {
			scope.Errorf("Error appying transform function: %v", err)
			return false
		}
		return a.collection.Set(ev.Entry.ID, i)

	case resource.Deleted:
		return a.collection.Remove(ev.Entry.ID.FullName)

	default:
		scope.Errorf("Unknown event kind encountered when processing %q: %v", ev.Entry.ID.String(), ev.Kind)
		return false
	}
}
