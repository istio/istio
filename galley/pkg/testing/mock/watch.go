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

package mock

import (
	"sync"

	"k8s.io/apimachinery/pkg/watch"
)

var (
	defaultWatchQueueSize = 1024 * 10
)

// Watch is a mock implementation of watch.Interface.
type Watch struct {
	ch       chan watch.Event
	q        []watch.Event
	qIndex   int
	qcond    *sync.Cond
	stopping bool
	stopCh   chan struct{}
}

type Watches []*Watch

func (arr Watches) Send(event watch.Event) {
	for _, w := range arr {
		w.Send(event)
	}
}

var _ watch.Interface = &Watch{}

// NewWatch returns a new Watch instance.
func NewWatch() *Watch {
	w := &Watch{
		ch:     make(chan watch.Event),
		q:      make([]watch.Event, defaultWatchQueueSize),
		qcond:  sync.NewCond(&sync.Mutex{}),
		stopCh: make(chan struct{}, 1),
	}

	go w.run()

	return w
}

// Stop is an implementation of watch.Interface.Watch.
func (w *Watch) Stop() {
	w.qcond.L.Lock()
	if !w.stopping {
		w.stopping = true
		close(w.stopCh)
		w.qcond.Signal()
	}
	w.qcond.L.Unlock()
}

// ResultChan is an implementation of watch.Interface.ResultChan.
func (w *Watch) ResultChan() <-chan watch.Event {
	return w.ch
}

// Send a watch event through the result channel.
func (w *Watch) Send(event watch.Event) {
	w.qcond.L.Lock()

	// Add the element to the queue. Avoiding append if possible to avoid extra array allocation.
	if w.qIndex < len(w.q) {
		w.q[w.qIndex] = event
	} else {
		w.q = append(w.q, event)
	}
	w.qIndex++

	w.qcond.Signal()
	w.qcond.L.Unlock()
}

func (w *Watch) run() {
	// Only the sender can close the channel safely.
	defer close(w.ch)

	tempQ := make([]watch.Event, defaultWatchQueueSize)
	for {
		w.qcond.L.Lock()

		if !w.stopping && w.qIndex == 0 {
			// Wait until we have an event.
			w.qcond.Wait()
		}

		if w.stopping {
			w.qcond.L.Unlock()
			return
		}

		// Copy all of the current events to tempQ
		numCopied := copy(tempQ, w.q[:w.qIndex])
		if numCopied < w.qIndex {
			// We've filled tempQ, but there are still elements remaining. Append them and allow
			// tempQ to grow appropriately.
			tempQ = append(tempQ, w.q[numCopied:w.qIndex]...)
			numCopied = w.qIndex
		}
		// We've emptied the queue - reset the index.
		w.qIndex = 0

		w.qcond.L.Unlock()

		// Push all of the events to the channel.
		for i := 0; i < numCopied; i++ {
			select {
			case <-w.stopCh:
				// Just return since we're shutting down.
				return
			case w.ch <- tempQ[i]:
			}
		}
	}
}
