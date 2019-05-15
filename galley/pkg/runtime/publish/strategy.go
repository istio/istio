// Copyright 2018 Istio Authors
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

package publish

import (
	"context"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/runtime/log"
	"istio.io/istio/galley/pkg/runtime/monitoring"
	"istio.io/istio/galley/pkg/util"
)

const (
	// Maximum wait time before deciding to publish the events.
	defaultMaxWaitDuration = time.Second

	// Minimum time distance between two events for deciding on the quiesce point. If the time delay
	// between two events is larger than this, then we can deduce that we hit a quiesce point.
	defaultQuiesceDuration = time.Second

	// The frequency for firing the timer events.
	defaultTimerFrequency = 500 * time.Millisecond
)

// Strategy is a heuristic model for deciding when to publish snapshots. It tries to detect
// quiesce points for events with a total bounded wait time.
type Strategy struct {
	maxWaitDuration time.Duration
	quiesceDuration time.Duration
	timerFrequency  time.Duration

	// stateLock protects the internal state of the publishing strategy.
	stateLock sync.Mutex

	// Publish channel is used to trigger the publication of snapshots.
	Publish chan struct{}

	// the time of first event that is received.
	firstEvent time.Time

	// the time of the latest event that is received.
	latestEvent time.Time

	// timer that is used for periodically checking for the quiesce point.
	timer *time.Timer

	// nowFn is a testing hook for overriding time.Now()
	nowFn func() time.Time

	// startTimerFn is a testing hook for overriding the starting of the timer.
	startTimerFn func()

	// worker manages the lifecycle of the timer worker thread.
	worker *util.Worker

	// resetChan is used to issue a reset to the timer.
	resetChan chan struct{}

	// pendingChanges indicates that there are unpublished changes.
	pendingChanges bool
}

// NewStrategyWithDefaults creates a new strategy with default values.
func NewStrategyWithDefaults() *Strategy {
	return NewStrategy(defaultMaxWaitDuration, defaultQuiesceDuration, defaultTimerFrequency)
}

// NewStrategy creates a new strategy with the given values.
func NewStrategy(
	maxWaitDuration time.Duration,
	quiesceDuration time.Duration,
	timerFrequency time.Duration) *Strategy {

	s := &Strategy{
		maxWaitDuration: maxWaitDuration,
		quiesceDuration: quiesceDuration,
		timerFrequency:  timerFrequency,
		Publish:         make(chan struct{}, 1),
		nowFn:           time.Now,
		worker:          util.NewWorker("runtime publishing strategy", log.Scope),
		resetChan:       make(chan struct{}, 1),
	}
	s.startTimerFn = s.startTimer
	return s
}

func (s *Strategy) OnChange() {
	s.stateLock.Lock()

	monitoring.RecordStrategyOnChange()

	// Capture the latest event time.
	s.latestEvent = s.nowFn()

	if !s.pendingChanges {
		// This is the first event after a quiesce, start a timer to periodically check event
		// frequency and fire the publish event.
		s.pendingChanges = true
		s.firstEvent = s.latestEvent

		// Start or reset the timer.
		if s.timer != nil {
			// Timer has already been started, just reset it now.
			// NOTE: Unlocking the state lock first, to avoid a potential race with
			// the timer thread waiting to enter onTimer.
			s.stateLock.Unlock()
			s.resetChan <- struct{}{}
			return
		}
		s.startTimerFn()
	}

	s.stateLock.Unlock()
}

// startTimer performs a start or reset on the timer. Called with lock on stateLock.
func (s *Strategy) startTimer() {
	s.timer = time.NewTimer(s.timerFrequency)

	eventLoop := func(ctx context.Context) {
		for {
			select {
			case <-s.timer.C:
				if !s.onTimer() {
					// We did not publish. Reset the timer and try again later.
					s.timer.Reset(s.timerFrequency)
				}
			case <-s.resetChan:
				s.timer.Reset(s.timerFrequency)
			case <-ctx.Done():
				// User requested to stop the timer.
				s.timer.Stop()
				return
			}
		}
	}

	// Start a go routine to listen to the timer.
	_ = s.worker.Start(nil, eventLoop)
}

func (s *Strategy) onTimer() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	now := s.nowFn()

	// If there has been a long time since the first event, or if there was a quiesce since last event,
	// then fire publish to create new snapshots.
	// Otherwise, reset the timer and get a call again.

	maxTimeReached := now.After(s.firstEvent.Add(s.maxWaitDuration))
	quiesceTimeReached := now.After(s.latestEvent.Add(s.quiesceDuration))

	published := false
	if maxTimeReached || quiesceTimeReached {
		// Try to send to the channel
		select {
		case s.Publish <- struct{}{}:
			s.pendingChanges = false
			published = true
		default:
			// If the calling code is not draining the publish channel, then we can potentially cause
			// a deadlock here. Avoid the deadlock by going through the timer loop again.
			log.Scope.Warnf("Unable to publish to the channel, resetting the timer again to avoid deadlock")
		}
	}

	monitoring.RecordOnTimer(maxTimeReached, quiesceTimeReached, !published)
	return published
}

func (s *Strategy) Close() {
	s.worker.Stop()
}
