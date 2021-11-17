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
	"fmt"
	"sync"

	"istio.io/istio/pkg/config/event"
)

// Accumulator accumulates events that is dispatched to it.
type Accumulator struct {
	mu sync.Mutex

	events []event.Event
}

// Handle implements event.Handler
func (a *Accumulator) Handle(e event.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.events = append(a.events, e)
}

// Events return current set of accumulated events.
func (a *Accumulator) Events() []event.Event {
	a.mu.Lock()
	defer a.mu.Unlock()

	r := make([]event.Event, len(a.events))
	copy(r, a.events)

	return r
}

// EventsWithoutOrigins calls Events, strips the origin fields from the embedded resources and returns result.
func (a *Accumulator) EventsWithoutOrigins() []event.Event {
	a.mu.Lock()
	defer a.mu.Unlock()

	events := make([]event.Event, len(a.events))
	copy(events, a.events)

	for i := 0; i < len(events); i++ {
		e := events[i].Resource
		if e != nil {
			e.Origin = nil
		}
		events[i].Resource = e
	}
	return events
}

// Clear all currently accummulated events.
func (a *Accumulator) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.events = nil
}

func (a *Accumulator) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var result string
	for _, e := range a.events {
		result += fmt.Sprintf("%v\n", e)
	}

	return result
}
