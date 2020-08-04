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

// Handler handles an incoming resource event.
type Handler interface {
	Handle(e Event)
}

// SentinelHandler is a special handler that does nothing with the event. It is useful to avoid
// nil checks on Handler fields. Specialized operations, such as CombineHandlers recognize SentinelHandlers
// and elide them when merging.
func SentinelHandler() Handler {
	return &sentinelInstance
}

var sentinelInstance sentinelHandler

type sentinelHandler struct{}

var _ Handler = &sentinelHandler{}

// Handle implements Handler
func (s *sentinelHandler) Handle(_ Event) {}

// HandlerFromFn returns a new Handler, based on the Handler function.
func HandlerFromFn(fn func(e Event)) Handler {
	return &fnHandler{
		fn: fn,
	}
}

var _ Handler = &fnHandler{}

type fnHandler struct {
	fn func(e Event)
}

// Handle implements Handler.
func (h *fnHandler) Handle(e Event) {
	h.fn(e)
}
