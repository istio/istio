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

import "sync"

// Source is an event source for a single collection.
//
// - A Source can be started/stopped multiple times, idempotently.
// - Every time a Source is started, it is expected to send the full list of events, including a FullSync event for
// each collection.
// - It must halt its dispatch of events before the Stop() call returns. The callers will assume that
// once Stop() returns, none of the registered handlers will receive any new events from this source.
type Source interface {
	Dispatcher

	// Start sending events.
	Start()

	// Stop sending events.
	Stop()
}

type compositeSource struct {
	mu      sync.Mutex
	sources []Source
}

var _ Source = &compositeSource{}

// Start implements Source
func (s *compositeSource) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		src.Start()
	}
}

// Stop implements Source
func (s *compositeSource) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		src.Stop()
	}
}

// Dispatch implements Source
func (s *compositeSource) Dispatch(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		src.Dispatch(h)
	}
}

// CombineSources combines multiple Sources and returns it as a single Source
func CombineSources(s ...Source) Source {
	return &compositeSource{
		sources: s,
	}
}
