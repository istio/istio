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

package runtime2

import (
	"time"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/log"
)

// startWatch registers with store, initiates a watch, and returns the current config state.
func startWatch(s store.Store, kinds map[string]proto.Message) (
	map[store.Key]*store.Resource, <-chan store.Event, error) {

	if err := s.Init(kinds); err != nil {
		return nil, nil, err
	}
	// create channel before listing.
	watchChan, err := s.Watch()
	if err != nil {
		return nil, nil, err
	}
	return s.List(), watchChan, nil
}

var watchFlushDuration = time.Second

// maxEvents is the likely maximum number of events
// we can expect in a second. It is used to avoid slice reallocation.
const maxEvents = 50

// applyEventsFn is used for testing
type applyEventsFn func(events []*store.Event)

// watchChanges watches for changes on a channel and
// publishes a batch of changes via applyEvents.
// watchChanges is started in a goroutine.
func watchChanges(wch <-chan store.Event, stop <-chan struct{}, applyEvents applyEventsFn) {
	// consume changes and apply them to data indefinitely
	var timeChan <-chan time.Time
	var timer *time.Timer
	events := make([]*store.Event, 0, maxEvents)

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
