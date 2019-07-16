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
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Selector distributes events to multiple different handlers, based on collection name.
type Selector struct {
	handlers map[collection.Name]Handler
}

var _ Handler = &Selector{}

// NewSelector returns a new instance of selector
func NewSelector() *Selector {
	return &Selector{
		handlers: make(map[collection.Name]Handler),
	}
}

// Handle implements handler
func (s *Selector) Handle(e Event) {
	scope.Processing.Debugf(">>> Selector.Handle: %+v", e)
	h, found := s.handlers[e.Source]
	if found {
		h.Handle(e)
	} else {
		scope.Processing.Debugf("!!! Selector.Handle: No handler for event, dropping: %v", e)
	}
}

// Select dispatches events for the selected collection.
func (s *Selector) Select(c collection.Name, h Handler) {
	existing, found := s.handlers[c]
	if !found {
		s.handlers[c] = h
	} else {
		s.handlers[c] = CombineHandlers(existing, h)
	}
}
