// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package event

import (
	"sync/atomic"

	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Transformer is a Processor that transforms input events from one or more collections to a set of output events to
// one or more collections.
//
// - A transformer must declare its inputs and outputs collections. via Inputs and Outputs methods. These must return
// idempotent results.
// - For every output collection that Transformer exposes, it must send a FullSync event, once the Transformer is
// started.
//
type Transformer interface {
	Processor

	// DispatchFor registers the given handler for a particular output collection.
	DispatchFor(c collection.Name, h Handler)

	// Inputs for this transformer
	Inputs() collection.Names

	// Outputs for this transformer
	Outputs() collection.Names
}

// FnTransform is a base type for handling common Transformer operations.
type FnTransform struct {
	in       collection.Names
	out      collection.Names
	selector Router
	startFn  func()
	stopFn   func()
	handleFn func(e Event, h Handler)
	syncCtr  int32
}

// Inputs partially implements Transformer
func (t *FnTransform) Inputs() collection.Names {
	return t.in
}

// Outputs partially implements Transformer
func (t *FnTransform) Outputs() collection.Names {
	return t.out
}

// Start implements Transformer
func (t *FnTransform) Start() {
	scope.Processing.Debug("FnTransform.Start")
	if t.selector == nil {
		t.selector = NewRouter()
	}

	atomic.StoreInt32(&t.syncCtr, int32(len(t.in)))

	if t.startFn != nil {
		t.startFn()
	}
}

// Stop implements Transformer
func (t *FnTransform) Stop() {
	scope.Processing.Debug("FnTransform.Stop")
	if t.stopFn != nil {
		t.stopFn()
	}
}

// DispatchFor implements Transformer
func (t *FnTransform) DispatchFor(c collection.Name, h Handler) {
	scope.Processing.Debugf("FnTransform.DispatchFor: %v => %T", c, h)
	t.selector = AddToRouter(t.selector, c, h)
}

// Handle implements Transformer
func (t *FnTransform) Handle(e Event) {
	if e.Kind == Reset {
		t.selector.Broadcast(e)
		return
	}

	if !e.IsSourceAny(t.in...) {
		scope.Processing.Warnf("Event with unexpected source received: %v", e)
		return
	}

	if e.Kind == FullSync {
		for {
			old := atomic.LoadInt32(&t.syncCtr)
			swapped := atomic.CompareAndSwapInt32(&t.syncCtr, old, old-1)
			if swapped {
				if old == 1 {
					// Limit reached to 0.
					t.selector.Broadcast(e)
				}
				break
			}
		}
		return
	}

	t.handleFn(e, t.selector)
}

// NewFnTransform returns a Transformer based on the given start, stop and input event handler functions.
func NewFnTransform(inputs, outputs collection.Names, startFn, stopFn func(), fn func(e Event, handler Handler)) *FnTransform {
	return &FnTransform{
		in:       inputs,
		out:      outputs,
		startFn:  startFn,
		stopFn:   stopFn,
		handleFn: fn,
	}
}
