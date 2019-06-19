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

package strategy

import (
	"sync"
	"time"
)

const (
	// Maximum wait time before deciding to publish the events.
	defaultMaxWaitDuration = time.Second * 2

	// Minimum time distance between two events for deciding on the quiesce point. If the time delay
	// between two events is larger than this, then we can deduce that we hit a quiesce point.
	defaultQuiesceDuration = time.Second / 2
)

// Debounce is a heuristic model for deciding when to publish snapshots. It tries to detect
// quiesce points for events with a total bounded wait time.
type Debounce struct {
	mu sync.Mutex

	maxWaitDuration time.Duration
	quiesceDuration time.Duration

	changeCh chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}

	reporter DebounceReporter
}

var _ Instance = &Debounce{}

// NewDebounceWithDefaults creates a new debounce strategy with default values.
func NewDebounceWithDefaults(r DebounceReporter) *Debounce {
	return NewDebounce(r, defaultMaxWaitDuration, defaultQuiesceDuration)
}

// NewDebounce creates a new debounce strategy with the given values.
func NewDebounce(r DebounceReporter, maxWaitDuration, quiesceDuration time.Duration) *Debounce {
	return &Debounce{
		maxWaitDuration: maxWaitDuration,
		quiesceDuration: quiesceDuration,
		changeCh:        make(chan struct{}, 1),
		reporter:        r,
	}
}

// Start implements Instance
func (d *Debounce) Start(fn OnSnapshotFn) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopCh != nil {
		scope.Debug("Debounce.Start: already started")
		return
	}
	d.stopCh = make(chan struct{})
	d.doneCh = make(chan struct{})

	// Drain the changeCh, to avoid events froma previous incarnation.
	drainCh(d.changeCh)

	go d.run(d.stopCh, d.doneCh, fn)
}

// Stop implements Instance
func (d *Debounce) Stop() {
	d.mu.Lock()

	if d.stopCh != nil {
		scope.Debug("Debounce.Stop: stopping")
		close(d.stopCh)
		d.stopCh = nil
	} else {
		scope.Debug("Debounce.Stop: already stopped")
	}
	d.mu.Unlock()

	<-d.doneCh
}

func (d *Debounce) run(stopCh, doneCh chan struct{}, fn OnSnapshotFn) {
	var maxDurationTimer *time.Timer
	var quiesceTimer *time.Timer

mainloop:
	for {
		select {
		case <-stopCh:
			break mainloop

		case <-d.changeCh:
			d.reporter.RecordStrategyOnChange()
			// fallthrough to start the timer.
		}

		maxDurationTimer = time.NewTimer(d.maxWaitDuration)
		quiesceTimer = time.NewTimer(d.quiesceDuration)

	loop:
		for {
			select {
			case <-stopCh:
				break mainloop

			case <-d.changeCh:
				d.reporter.RecordStrategyOnChange()

				quiesceTimer.Stop()
				drainTimeCh(quiesceTimer.C)
				quiesceTimer.Reset(d.quiesceDuration)
				d.reporter.RecordTimerReset()

			case <-quiesceTimer.C:
				d.reporter.RecordOnQuiesceTimer()
				break loop

			case <-maxDurationTimer.C:
				d.reporter.RecordOnMaxTimer()
				break loop
			}
		}

		quiesceTimer.Stop()
		drainTimeCh(quiesceTimer.C)
		maxDurationTimer.Stop()
		drainTimeCh(maxDurationTimer.C)
		fn()
	}

	close(doneCh)
}

// OnChange implements Instance
func (d *Debounce) OnChange() {
	select {
	case d.changeCh <- struct{}{}:
	default:
	}
}

func drainCh(ch chan struct{}) {
loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break loop
			}
		default:
			break loop

		}
	}
}

func drainTimeCh(ch <-chan time.Time) {
loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break loop
			}
		default:
			break loop

		}
	}
}
