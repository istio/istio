//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain accumulator copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("processing", "Galley data processing", 0)

// Dispatcher is a Handler that can dispatch events to other Handlers, based on Collection.
type Dispatcher struct {
	handlers map[resource.Collection][]Handler
}

var _ Handler = &Dispatcher{}

// Handle implements Handler
func (d *Dispatcher) Handle(e resource.Event) {
	handlers, found := d.handlers[e.Entry.ID.Collection]
	if !found {
		scope.Warnf("Unhandled resource event: %v", e)
		return
	}

	for _, h := range handlers {
		h.Handle(e)
	}
}

// DispatcherBuilder builds Dispatchers
type DispatcherBuilder struct {
	handlers map[resource.Collection][]Handler
}

// NewDispatcherBuilder returns a new dispatcher dispatcher
func NewDispatcherBuilder() *DispatcherBuilder {
	return &DispatcherBuilder{
		handlers: make(map[resource.Collection][]Handler),
	}
}

// Add a new handler for the given Collection
func (d *DispatcherBuilder) Add(t resource.Collection, h Handler) {
	handlers := d.handlers[t]
	handlers = append(handlers, h)
	d.handlers[t] = handlers
}

// Build a Dispatcher
func (d *DispatcherBuilder) Build() *Dispatcher {
	r := &Dispatcher{
		handlers: d.handlers,
	}
	d.handlers = nil
	return r
}
