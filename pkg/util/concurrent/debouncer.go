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

package concurrent

import (
	"time"

	"istio.io/istio/pkg/util/sets"
)

type Debouncer[T comparable] struct{}

func (d *Debouncer[T]) Run(ch chan T, stopCh <-chan struct{}, debounceMinInterval, debounceMaxInterval time.Duration, pushFn func(sets.Set[T])) {
	var timeChan <-chan time.Time
	var startDebounce time.Time
	var lastConfigUpdateTime time.Time

	pushCounter := 0
	debouncedEvents := 0

	// Keeps track of the push requests. If updates are debounce they will be merged.
	combinedEvents := sets.New[T]()

	free := true
	freeCh := make(chan struct{}, 1)

	push := func(events sets.Set[T], debouncedEvents int, startDebounce time.Time) {
		pushFn(events)
		freeCh <- struct{}{}
	}

	pushWorker := func() {
		eventDelay := time.Since(startDebounce)
		quietTime := time.Since(lastConfigUpdateTime)
		// it has been too long or quiet enough
		if eventDelay >= debounceMaxInterval || quietTime >= debounceMinInterval {
			if combinedEvents != nil {
				pushCounter++
				free = false
				go push(combinedEvents, debouncedEvents, startDebounce)
				combinedEvents = sets.New[T]()
				debouncedEvents = 0
			}
		} else {
			timeChan = time.After(debounceMinInterval - quietTime)
		}
	}

	for {
		select {
		case <-freeCh:
			free = true
			pushWorker()
		case r := <-ch:

			lastConfigUpdateTime = time.Now()
			if debouncedEvents == 0 {
				timeChan = time.After(debounceMinInterval)
				startDebounce = lastConfigUpdateTime
			}
			debouncedEvents++

			combinedEvents = combinedEvents.Insert(r)
		case <-timeChan:
			if free {
				pushWorker()
			}
		case <-stopCh:
			return
		}
	}
}
