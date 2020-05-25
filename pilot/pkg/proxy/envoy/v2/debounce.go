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

import "time"

type debouncer interface {
	newRequest() (bool, time.Duration)
	debounceRequest() (bool, int, time.Duration)
	quietTime() time.Duration
	eventDelay() time.Duration
}

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

func newFixedDebouncer(debounceAfter, debounceMax time.Duration) debouncer {
	return fixedDebouncer{
		debounceAfter: debounceAfter,
		debounceMax:   debounceMax,
	}
}

func (fd fixedDebouncer) newRequest() (bool, time.Duration) {
	fd.lastConfigUpdateTime = time.Now()
	if fd.debouncedEvents == 0 {
		fd.startDebounce = fd.lastConfigUpdateTime
	}
	fd.debouncedEvents++
	return false, fd.debounceAfter
}

func (fd fixedDebouncer) debounceRequest() (bool, int, time.Duration) {
	de := fd.debouncedEvents
	// it has been too long or quiet enough
	if fd.eventDelay() >= fd.debounceMax || fd.quietTime() >= fd.debounceAfter {
		fd.debouncedEvents = 0
		return true, de, 0
	} else {
		return false, de, fd.debounceAfter - fd.quietTime()
	}
}

func (fd fixedDebouncer) quietTime() time.Duration {
	return time.Since(fd.startDebounce)
}

func (fd fixedDebouncer) eventDelay() time.Duration {
	return time.Since(fd.lastConfigUpdateTime)
}
