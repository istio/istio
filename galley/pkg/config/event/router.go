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

	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/scope"
)

// Router distributes events to different handlers, based on collection name.
type Router interface {
	Handler
	Broadcast(e Event)
}

// emptyRouter
type emptyRouter struct {
}

var _ Router = &emptyRouter{}

// Handle implements Router
func (r *emptyRouter) Handle(_ Event) {}

// Broadcast implements Router
func (r *emptyRouter) Broadcast(_ Event) {}

type singleRouter struct {
	source  collection.Name
	handler Handler
}

var _ Router = &singleRouter{}

// Handle implements Handler
func (r *singleRouter) Handle(e Event) {
	if e.Kind == Reset || e.IsSource(r.source) {
		r.handler.Handle(e)
	}
}

// Broadcast implements Router
func (r *singleRouter) Broadcast(e Event) {
	e = e.WithSource(r.source)
	r.handler.Handle(e)
}

// Router distributes events to multiple different handlers, based on collection name.
type router struct {
	handlers map[collection.Name]Handler
}

var _ Router = &router{}

// Handle implements Handler
func (r *router) Handle(e Event) {
	h, found := r.handlers[e.Source]
	if found {
		h.Handle(e)
	} else {
		scope.Processing.Warna("Router.Handle: No handler for event, dropping: ", e)
	}
}

// Broadcast implements Router
func (r *router) Broadcast(e Event) {
	for d, h := range r.handlers {
		e = e.WithSource(d)
		h.Handle(e)
	}
}

// NewRouter returns a new instance of Router
func NewRouter() Router {
	return &emptyRouter{}
}

// AddToRouter adds the given handler for the given source collection.
func AddToRouter(r Router, source collection.Name, handler Handler) Router {
	if r == nil {
		return &singleRouter{
			source:  source,
			handler: handler,
		}
	}

	switch v := r.(type) {
	case *emptyRouter:
		return &singleRouter{
			source:  source,
			handler: handler,
		}

	case *singleRouter:
		if v.source == source {
			return &singleRouter{
				source:  source,
				handler: CombineHandlers(v.handler, handler),
			}
		}
		s := &router{
			handlers: make(map[collection.Name]Handler),
		}
		s.handlers[v.source] = v.handler
		s.handlers[source] = handler
		return s

	case *router:
		s := &router{
			handlers: make(map[collection.Name]Handler),
		}
		for k, v := range v.handlers {
			s.handlers[k] = v
		}
		old := s.handlers[source]
		s.handlers[source] = CombineHandlers(old, handler)
		return s

	default:
		panic(fmt.Sprintf("unknown Router: %T", v))
	}
}
