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

package v2

import (
	"sync"
	"time"

	"istio.io/istio/pilot/pkg/model"
)

// DebouncedEvent is used as entry type, which is enqueued in DebouncedChannel
type DebouncedEvent = interface{}

// Notification is used to enqueue the DebouncedEvent
type Notification struct {
	// Event is the merged result of all events.
	Event DebouncedEvent
	// Ack must be called to acknowledge the processing of the event.
	Ack func()
}

// DebouncedChannel is a channel which abstracts the debounce algorithm
type DebouncedChannel struct {
	// C channel, which can be used in selects. It delivers debounced events.
	// The function emitted must be called. Otherwise processing is stopped.
	C <-chan Notification
	c chan Notification

	debounceAfterEnd time.Time
	debounceMaxEnd   time.Time
	lastEmitTime     time.Time
	debounceAfter    time.Duration
	debounceMax      time.Duration
	mutex            sync.Mutex

	merge              func(accumulator DebouncedEvent, value DebouncedEvent) DebouncedEvent
	accumulator        DebouncedEvent
	initialAccumulator DebouncedEvent

	// pushCounter is used to collect the overall number of pushes to the channel C
	pushCounter uint32
	// debounceCounter is used to count the number of events, which are merged together into one push to channel C
	debounceCounter uint32
	ackID           uint32
	running         bool
}

// NewDebouncedChannel create a new instance of DebouncedChannel
// debounceAfter: A new outgoing event is emitted if no additional event is enqueued during this period (resets on every enqueued event).
// debounceMax: A new outgoing event is emitted after this period (even if additional events were enqueued in that period).
// initialAccumulator: Initial value of the accumulator
// merge: function, which is called to merge two events if they are debounced.
func NewDebouncedChannel(debounceAfter time.Duration, debounceMax time.Duration,
	initialAccumulator DebouncedEvent, merge func(accumulator DebouncedEvent, value DebouncedEvent) DebouncedEvent) *DebouncedChannel {

	c := make(chan Notification, 1)

	return &DebouncedChannel{
		C:                  c,
		c:                  c,
		initialAccumulator: initialAccumulator,
		accumulator:        initialAccumulator,
		merge:              merge,
		debounceAfter:      debounceAfter,
		debounceMax:        debounceMax,
		lastEmitTime:       time.Unix(0, 0),
	}

}

// Enqueue pushes a new event to the channel
func (dc *DebouncedChannel) Enqueue(element DebouncedEvent) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	dc.accumulator = dc.merge(dc.accumulator, element)
	if dc.debounceCounter == 0 {
		dc.debounceMaxEnd = time.Now().Add(dc.debounceMax)
		go func() {
			dc.mutex.Lock()
			defer dc.mutex.Unlock()

			for {
				debounceCounter := dc.debounceCounter

				sleepDuration := min(dc.debounceAfter, time.Until(dc.debounceMaxEnd))
				dc.mutex.Unlock()
				time.Sleep(sleepDuration)
				dc.mutex.Lock()

				if debounceCounter == dc.debounceCounter || time.Now().After(dc.debounceMaxEnd) {
					break
				}
			}
			// Check if the notification should be sent after the timeout expired
			dc.runIfNeededLocked()
		}()
	}

	dc.debounceAfterEnd = time.Now().Add(dc.debounceAfter)
	dc.debounceCounter++

	// Check if the notification should be sent immediately
	dc.runIfNeededLocked()
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// runIfNeededLocked must be called in a locked section
func (dc *DebouncedChannel) runIfNeededLocked() {
	if dc.debounceCounter == 0 {
		// No events stored. Therefore nothing to do
		return
	}
	if dc.running {
		// There is already an event being processed. We are still waiting for the acknowledgement.
		return
	}
	if dc.debounceMaxEnd.Before(time.Now()) || dc.debounceAfterEnd.Before(time.Now()) {
		// It's time to notify the worker

		accumulator := dc.accumulator
		debounceCounter := dc.debounceCounter
		pushCounter := dc.pushCounter
		lastEmitTime := dc.lastEmitTime

		dc.running = true
		dc.debounceCounter = 0
		dc.pushCounter++
		dc.accumulator = dc.initialAccumulator
		dc.lastEmitTime = time.Now()

		adsLog.Infof("Push debounce stable[%d] %d: %v since last change, %v since last push, accumulator=%v",
			pushCounter, debounceCounter, time.Since(lastEmitTime), lastEmitTime, accumulator)

		ackID := dc.ackID
		notification := Notification{Event: accumulator, Ack: func() {
			dc.ack(ackID)
		}}
		dc.c <- notification
	}
}

func (dc *DebouncedChannel) ack(ackID uint32) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	if ackID == dc.ackID {
		dc.running = false
		dc.ackID++
		// Check if there are any new events that need to be notified while we were busy.
		dc.runIfNeededLocked()
	}
}

// NewUpdateRequestDebouncedChannel create a new instance of DebouncedChannel
// For details see NewDebouncedChannel
func NewUpdateRequestDebouncedChannel(debounceAfter time.Duration, debounceMax time.Duration, initialAccumulator *model.UpdateRequest,
	merge func(accumulator *model.UpdateRequest, value *model.UpdateRequest) *model.UpdateRequest) *DebouncedChannel {

	return NewDebouncedChannel(debounceAfter, debounceMax, initialAccumulator, func(accumulator DebouncedEvent, value DebouncedEvent) DebouncedEvent {
		return merge(accumulator.(*model.UpdateRequest), value.(*model.UpdateRequest))
	})
}
