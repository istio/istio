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

package krt

import (
	"istio.io/istio/pkg/slices"
)

// BatchedEventFilter allows an event handler to have alternative event suppression mechanics to filter out unnecessary events.
// For instance, I can make a transformation from `object => object.name` to only trigger events for changes to the name;
// the output will be compared (using standard equality checking), and only changes will trigger the handler.
// Note this is in addition to the normal event mechanics, so this can only filter things further.
func BatchedEventFilter[I, O any](cf func(a I) O, handler func(events []Event[I], initialSync bool)) func(o []Event[I], initialSync bool) {
	return func(events []Event[I], initialSync bool) {
		ev := slices.Filter(events, func(e Event[I]) bool {
			if e.Old != nil && e.New != nil {
				if equal(cf(*e.Old), cf(*e.New)) {
					// Equal under conversion, so we can skip
					return false
				}
			}
			return true
		})
		if len(ev) == 0 {
			return
		}
		handler(ev, initialSync)
	}
}
