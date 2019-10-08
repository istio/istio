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

package fixtures

import (
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

// Transformer implements event.Transformer for testing purposes.
type Transformer struct {
	Handlers          map[collection.Name]event.Handler
	Started           bool
	InputCollections  collection.Names
	OutputCollections collection.Names

	fn func(tr *Transformer, e event.Event)
}

var _ event.Transformer = &Transformer{}

// NewTransformer returns a new fixture.Transformer.
func NewTransformer(inputs, outputs collection.Names, handlerFn func(tr *Transformer, e event.Event)) *Transformer {
	return &Transformer{
		InputCollections:  inputs,
		OutputCollections: outputs,
		Handlers:          make(map[collection.Name]event.Handler),
		fn:                handlerFn,
	}
}

// Start implements event.Transformer
func (t *Transformer) Start() {
	t.Started = true
}

// Stop implements event.Transformer
func (t *Transformer) Stop() {
	t.Started = false
}

// Handle implements event.Transformer
func (t *Transformer) Handle(e event.Event) {
	t.fn(t, e)
}

// DispatchFor implements event.Transformer
func (t *Transformer) DispatchFor(c collection.Name, h event.Handler) {
	handlers := t.Handlers[c]
	handlers = event.CombineHandlers(handlers, h)
	t.Handlers[c] = handlers
}

// Inputs implements event.Transformer
func (t *Transformer) Inputs() collection.Names {
	return t.InputCollections
}

// Outputs implements event.Transformer
func (t *Transformer) Outputs() collection.Names {
	return t.OutputCollections
}

// Publish a message to the given collection
func (t *Transformer) Publish(c collection.Name, e event.Event) {
	h := t.Handlers[c]
	if h != nil {
		h.Handle(e)
	}
}
