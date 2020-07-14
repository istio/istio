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

package store

import (
	"time"

	"istio.io/pkg/log"
)

// StartWatch registers with store, initiates a watch, and returns the current config state.
func StartWatch(s Store) (map[Key]*Resource, <-chan Event, error) {

	// create channel before listing.
	watchChan, err := s.Watch()
	if err != nil {
		return nil, nil, err
	}
	return s.List(), watchChan, nil
}

// maxEvents is the likely maximum number of events
// we can expect in a second. It is used to avoid slice reallocation.
const maxEvents = 50

// ApplyEventsFn is used for testing
type ApplyEventsFn func(events []*Event)

// WatchChanges watches for changes on a channel and
// publishes a batch of changes via applyEvents.
// WatchChanges is started in a goroutine.
func WatchChanges(wch <-chan Event, stop <-chan struct{}, watchFlushDuration time.Duration, applyEvents ApplyEventsFn) {
	// consume changes and apply them to data indefinitely
	var timeChan <-chan time.Time
	var timer *time.Timer
	events := make([]*Event, 0, maxEvents)

	for {
		select {
		case ev := <-wch:
			if len(events) == 0 {
				timer = time.NewTimer(watchFlushDuration)
				timeChan = timer.C
			}
			events = append(events, &ev)
		case <-timeChan:
			timer.Stop()
			timeChan = nil
			log.Infof("Publishing %d events", len(events))
			applyEvents(events)
			events = events[:0]
		case <-stop:
			return
		}
	}
}
