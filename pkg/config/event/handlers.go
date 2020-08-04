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

package event

// Handlers is a group of zero or more Handlers. Handlers is an instance of Handler, dispatching incoming events
// to the Handlers it contains.
type Handlers struct {
	handlers []Handler
}

var _ Handler = &Handlers{}

// Handle implements Handler
func (hs *Handlers) Handle(e Event) {
	for _, h := range hs.handlers {
		h.Handle(e)
	}
}

// Add a new handler to handlers
func (hs *Handlers) Add(h Handler) {
	hs.handlers = append(hs.handlers, h)
}

// Size returns number of handlers in this handler set.
func (hs *Handlers) Size() int {
	return len(hs.handlers)
}

// CombineHandlers combines two handlers into a single set of handlers and returns. If any of the Handlers is an
// instance of Handlers, then their contains will be flattened into a single list.
func CombineHandlers(h1 Handler, h2 Handler) Handler {
	if h1 == nil {
		return h2
	}

	if h2 == nil {
		return h1
	}

	if _, ok := h1.(*sentinelHandler); ok {
		return h2
	}

	if _, ok := h2.(*sentinelHandler); ok {
		return h1
	}

	var r Handlers
	if hs, ok := h1.(*Handlers); ok {
		r.handlers = append(r.handlers, hs.handlers...)
	} else {
		r.handlers = append(r.handlers, h1)
	}

	if hs, ok := h2.(*Handlers); ok {
		r.handlers = append(r.handlers, hs.handlers...)
	} else {
		r.handlers = append(r.handlers, h2)
	}

	return &r
}
