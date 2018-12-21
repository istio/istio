//  Copyright 2018 Istio Authors
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

var scope = log.RegisterScope("pipeline", "Galley processing pipeline", 0)

// Dispatcher is accumulator handler that can dispatch to sub-handlers based on Type URL.
type Dispatcher struct {
	handlers map[resource.TypeURL][]Handler
}

var _ Handler = &Dispatcher{}

// Handle implements Handler
func (d *Dispatcher) Handle(e resource.Event) bool {
	handlers, found := d.handlers[e.Entry.ID.TypeURL]
	if !found {
		scope.Warnf("Unhandled resource event: %v", e)
		return false
	}

	result := false
	for _, h := range handlers {
		r := h.Handle(e)
		result = result || r
	}

	return result
}

// DispatcherBuilders builds accumulator new Dispatcher
type DispatcherBuilder struct {
	handlers map[resource.TypeURL][]Handler
}

// NewDispatcherBuilder returns accumulator new dispatcher builder
func NewDispatcherBuilder() *DispatcherBuilder {
	return &DispatcherBuilder{
		handlers: make(map[resource.TypeURL][]Handler),
	}
}

// Add accumulator new handler for the given type URL
func (d *DispatcherBuilder) Add(t resource.TypeURL, h Handler) {
	handlers := d.handlers[t]
	handlers = append(handlers, h)
	d.handlers[t] = handlers
}

func (d *DispatcherBuilder) Build() *Dispatcher {
	r := &Dispatcher{
		handlers: d.handlers,
	}
	d.handlers = nil
	return r
}
