// Copyright 2020 Istio Authors
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

package v2

import (
	"math"
	"time"
)

// debouncer interface allows to plugin various debouncer implementations while pusning configuration to proxies.
type debouncer interface {
	// newRequest handles a new configuration. It returns whether the request should be debounced.
	// For the requess that needs to be debounced, it also returns the debounce delay.
	newRequest() (bool, time.Duration)

	// tryDebounce is invoked when the request is about to be processed. It returns whether the request should be debounced.
	// For the requess that needs to be debounced, it also returns the debounce delay.
	tryDebounce() (bool, time.Duration)

	// quietTime returns the delay from start i.e. first event till now.
	quietTime() time.Duration

	// eventDelay returns the delay since last update.
	eventDelay() time.Duration

	// events returns the number of config requests that were debounced till now.
	events() int
}

// fixedDebouncer is an implementation of debouncer, that debounces with a fixed and max delay.
type fixedDebouncer struct {
	// debounceAfter is the delay added to events to wait
	// after a registry/config event for debouncing.
	// This will delay the push by at least this interval, plus
	// the time getting subsequent events. If no change is
	// detected the push will happen, otherwise we'll keep
	// delaying until things settle.
	debounceAfter time.Duration

	// debounceMax is the maximum time to wait for events
	// while debouncing. Defaults to 10 seconds. If events keep
	// showing up with no break for this time, we'll trigger a push.
	debounceMax          time.Duration
	startDebounce        time.Time
	lastConfigUpdateTime time.Time
	debouncedEvents      int
}

// backoffDebouncer is an implementation of debouncer that pushes the first event immediately and
// applies backoff debouncing logic to subsequent events till a max debounce is reached.
type backoffDebouncer struct {
	baseDebounceDelay    time.Duration
	debounceMax          time.Duration
	lastConfigUpdateTime time.Time

	debouncedEvents int
	backOffFactor   int
}

// newFixedDebouncer builds a fixed debouncer.
func newFixedDebouncer(debounceAfter, debounceMax time.Duration) debouncer {
	return &fixedDebouncer{
		debounceAfter: debounceAfter,
		debounceMax:   debounceMax,
	}
}

// newBackoffDebouncer builds a backoff debouncer.
func newBackoffDebouncer(baseDebounceDelay, debounceMax time.Duration) debouncer {
	return &backoffDebouncer{
		baseDebounceDelay: baseDebounceDelay,
		debounceMax:       debounceMax,
		backOffFactor:     2,
	}
}

func (fd *fixedDebouncer) newRequest() (bool, time.Duration) {
	fd.lastConfigUpdateTime = time.Now()
	fd.debouncedEvents++
	if fd.debouncedEvents == 1 {
		fd.startDebounce = fd.lastConfigUpdateTime
	}
	return true, fd.debounceAfter
}

func (fd *fixedDebouncer) tryDebounce() (bool, time.Duration) {
	debounced := false
	// it has been too long or quiet enough
	if fd.eventDelay() >= fd.debounceMax || fd.quietTime() >= fd.debounceAfter {
		fd.debouncedEvents = 0
	} else {
		debounced = true
	}
	return debounced, fd.debounceAfter - fd.quietTime()
}

func (fd fixedDebouncer) quietTime() time.Duration {
	return time.Since(fd.startDebounce)
}

func (fd fixedDebouncer) eventDelay() time.Duration {
	return time.Since(fd.lastConfigUpdateTime)
}

func (fd fixedDebouncer) events() int {
	return fd.debouncedEvents
}

func (bd *backoffDebouncer) newRequest() (bool, time.Duration) {
	bd.lastConfigUpdateTime = time.Now()
	if bd.debouncedEvents == 0 {
		bd.debouncedEvents++
		return false, 0
	}
	delay := bd.backoffDelay()
	bd.debouncedEvents++
	return true, delay
}

func (bd *backoffDebouncer) tryDebounce() (bool, time.Duration) {
	// We have reached max debounce interval - we should not debounce further.
	if bd.eventDelay() >= bd.debounceMax {
		bd.debouncedEvents = 0
		return false, 0
	}
	return true, bd.backoffDelay()
}

func (bd backoffDebouncer) quietTime() time.Duration {
	return time.Since(bd.lastConfigUpdateTime)
}

func (bd backoffDebouncer) eventDelay() time.Duration {
	return time.Since(bd.lastConfigUpdateTime)
}

func (bd backoffDebouncer) events() int {
	return bd.debouncedEvents
}

func (bd backoffDebouncer) backoffDelay() time.Duration {
	return time.Duration(float64(bd.baseDebounceDelay) * math.Pow(float64(bd.backOffFactor), float64(bd.debouncedEvents-1)))
}
