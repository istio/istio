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

import "istio.io/istio/galley/pkg/config/event"

// Source is a test implementation of event.Source
type Source struct {
	Handlers event.Handler
	started  bool
}

var _ event.Source = &Source{}

// Dispatch implements event.Dispatcher
func (s *Source) Dispatch(h event.Handler) {
	s.Handlers = event.CombineHandlers(s.Handlers, h)
}

// Start implements event.Source
func (s *Source) Start() {
	s.started = true
}

// Stop implements event.Source
func (s *Source) Stop() {
	s.started = false
}
