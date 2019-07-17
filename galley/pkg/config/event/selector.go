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
	"fmt"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Selector distributes events to different handlers, based on collection name.
type Selector interface {
	Handler
	Broadcast(e Event)
}

// emptySelector
type emptySelector struct {
}

var _ Selector = &emptySelector{}

// Handle implements Selector
func (s *emptySelector) Handle(_ Event) {}

// Broadcast implements Selector
func (s *emptySelector) Broadcast(_ Event) {}

type singleSelector struct {
	target  collection.Name
	handler Handler
}

var _ Selector = &singleSelector{}

// Handle implements Handler
func (s *singleSelector) Handle(e Event) {
	if e.Kind == Reset || e.IsSource(s.target) {
		s.handler.Handle(e)
	}
}

// Broadcast implements Selector
func (s *singleSelector) Broadcast(e Event) {
	e = e.WithSource(s.target)
	s.handler.Handle(e)
}

// Selector distributes events to multiple different handlers, based on collection name.
type selector struct {
	handlers map[collection.Name]Handler
}

var _ Selector = &selector{}

// Handle implements Handler
func (s *selector) Handle(e Event) {
	scope.Processing.Debuga(">>> Selector.Handle: ", e)
	h, found := s.handlers[e.Source]
	if found {
		h.Handle(e)
	} else {
		scope.Processing.Warna("Selector.Handle: No handler for event, dropping: ", e)
	}
}

// Broadcast implements Selector
func (s *selector) Broadcast(e Event) {
	for d, h := range s.handlers {
		e = e.WithSource(d)
		h.Handle(e)
	}
}

// NewSelector returns a new instance of Selector
func NewSelector() Selector {
	return &emptySelector{}
}

// AddToSelector adds the given handler for selecting the target collection.
func AddToSelector(sel Selector, target collection.Name, handler Handler) Selector {
	if sel == nil {
		return &singleSelector{
			target:  target,
			handler: handler,
		}
	}

	switch v := sel.(type) {
	case *emptySelector:
		return &singleSelector{
			target:  target,
			handler: handler,
		}

	case *singleSelector:
		if v.target == target {
			return &singleSelector{
				target:  target,
				handler: CombineHandlers(v.handler, handler),
			}
		}
		s := &selector{
			handlers: make(map[collection.Name]Handler),
		}
		s.handlers[v.target] = v.handler
		s.handlers[target] = handler
		return s

	case *selector:
		s := &selector{
			handlers: make(map[collection.Name]Handler),
		}
		for k, v := range v.handlers {
			s.handlers[k] = v
		}
		old := s.handlers[target]
		s.handlers[target] = CombineHandlers(old, handler)
		return s

	default:
		panic(fmt.Sprintf("unkown Selector: %T", v))
	}
}
