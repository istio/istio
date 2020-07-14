// Copyright Istio Authors
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
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

// Transformer implements event.Transformer for testing purposes.
type Transformer struct {
	Handlers          map[collection.Name]*sourceAndHandler
	Started           bool
	InputCollections  collection.Schemas
	OutputCollections collection.Schemas

	fn func(tr *Transformer, e event.Event)
}

var _ event.Transformer = &Transformer{}

// NewTransformer returns a new fixture.Transformer.
func NewTransformer(inputs, outputs collection.Schemas, handlerFn func(tr *Transformer, e event.Event)) *Transformer {
	return &Transformer{
		InputCollections:  inputs,
		OutputCollections: outputs,
		Handlers:          make(map[collection.Name]*sourceAndHandler),
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
func (t *Transformer) DispatchFor(c collection.Schema, h event.Handler) {
	entry := t.Handlers[c.Name()]
	if entry == nil {
		t.Handlers[c.Name()] = &sourceAndHandler{
			source:  c,
			handler: h,
		}
		return
	}

	entry.handler = event.CombineHandlers(entry.handler, h)
}

// Inputs implements event.Transformer
func (t *Transformer) Inputs() collection.Schemas {
	return t.InputCollections
}

// Outputs implements event.Transformer
func (t *Transformer) Outputs() collection.Schemas {
	return t.OutputCollections
}

// Publish a message to the given collection
func (t *Transformer) Publish(c collection.Name, e event.Event) {
	h := t.Handlers[c]
	if h != nil {
		h.handler.Handle(e)
	}
}

type sourceAndHandler struct {
	source  collection.Schema
	handler event.Handler
}
