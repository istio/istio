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

package flow

import (
	"istio.io/istio/galley/pkg/runtime/resource"
)

// TransformEntryFn transforms an entry to a custom type, before storing it in a table.
type TransformEntryFn func(entry resource.Entry) (interface{}, error)

// Accumulator handles incoming events, optionally applies a conversion function, before setting the values
// in a backing table
type Accumulator struct {
	xform TransformEntryFn
	table *Table
}

var _ Handler = &Accumulator{}

// NewAccumulator returns a new Accumulator
func NewAccumulator(c *Table, xform TransformEntryFn) *Accumulator {
	if xform == nil {
		xform = func(entry resource.Entry) (interface{}, error) { return entry, nil }
	}

	return &Accumulator{
		xform: xform,
		table: c,
	}
}

// Handle implements Handler
func (a *Accumulator) Handle(ev resource.Event) {
	switch ev.Kind {
	case resource.Added, resource.Updated:
		i, err := a.xform(ev.Entry)
		if err != nil {
			scope.Errorf("Error appying transform function: %v", err)
			return
		}
		a.table.Set(ev.Entry.ID, i)

	case resource.Deleted:
		a.table.Remove(ev.Entry.ID.FullName)

	default:
		scope.Errorf("Unknown event kind encountered when processing %q: %v", ev.Entry.ID.String(), ev.Kind)
	}
}
