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
	"sort"
	"strings"

	"istio.io/istio/pkg/config/event"
)

// FilterFn is a function for filtering events
type FilterFn func(event []event.Event) []event.Event

// NoVersions strips the versions from the given events
func NoVersions(events []event.Event) []event.Event {
	result := make([]event.Event, len(events))
	copy(result, events)

	for i := range result {
		result[i].Resource = result[i].Resource.Clone()
		result[i].Resource.Metadata.Version = ""
	}

	return result
}

// NoFullSync filters FullSync events and returns.
func NoFullSync(events []event.Event) []event.Event {
	result := make([]event.Event, 0, len(events))

	for _, e := range events {
		if e.Kind == event.FullSync {
			continue
		}
		result = append(result, e)
	}

	return result
}

// Sort events in a stable order.
func Sort(events []event.Event) []event.Event {
	result := make([]event.Event, len(events))
	copy(result, events)

	sort.SliceStable(result, func(i, j int) bool {
		c := strings.Compare(result[i].String(), result[j].String())
		if c != 0 {
			return c < 0
		}

		if result[i].Resource == nil && result[j].Resource == nil {
			return false
		}

		if result[i].Resource == nil {
			return false
		}

		if result[j].Resource == nil {
			return true
		}

		return strings.Compare(fmt.Sprintf("%+v", result[i].Resource), fmt.Sprintf("%+v", result[j].Resource)) < 0
	})

	return result
}

// Chain filters back to back
func Chain(fns ...FilterFn) FilterFn {
	return func(e []event.Event) []event.Event {
		for _, fn := range fns {
			e = fn(e)
		}
		return e
	}
}
