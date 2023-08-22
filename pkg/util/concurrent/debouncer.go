package concurrent

import (
	"time"

	"istio.io/istio/pkg/util/sets"
)

type Debouncer[T comparable] struct {
}

func (d *Debouncer[T]) Run(ch chan T, stopCh <-chan struct{}, debounceMinInterval, debounceMaxInterval time.Duration, pushFn func(sets.Set[T])) {
	var timeChan <-chan time.Time
	var startDebounce time.Time
	var lastConfigUpdateTime time.Time

	pushCounter := 0
	debouncedEvents := 0

	// Keeps track of the push requests. If updates are debounce they will be merged.
	var combinedEvents sets.Set[T]

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
				combinedEvents = nil
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
