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

package status

import (
	"sync"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

// use a sentinel value as the last item in a work queue. This allows doing a simple null check on the next to
// detect whether a status is queued as work or not.
var sentinel = &status{}

// state is the core internal state of the status controller. It keeps track of lifecycle states, currently known &
// desired status for resources and a work-queue for the reconciliation loop.
//
// The state is changed through 3 access points:
// - Incoming watch events that allows observing of changes to the status field of resources.
// - New set of diagnostic messages that need to be applied as state.
// - Reconciliation worker, popping work items for performing actual status updates.
//
// Changes to the desired and/or observed state will cause desired/observed to go out of sync will cause a new work
// item to be created for the work queue. Initially, observed state changes will be ignored (see reconcile flag) as
// during Galley boot-up, we don't want to generate superfluous updates to existing status. Once status receives an
// update with a new set of diagnostic messages (which could be empty), the reconciliation will be enabled.
//
// To reduce memory pressure, the state will only keep track of desired/observed status if it is set.
//
// The work queue is implemented as a linked-list over the status objects. This reduces memory foot-print and also helps
// with scheduling work only once when things are updated successively.
//
// The worker(s) will get a copy of the needed update from the work queue and try to reconcile. If the reconciliation
// fails, they will need to explicitly put the entries back into the queue (TBD). If there are no outstanding work items
// for the reconciliation loop, then it will wait blocked in the dequeueWork call. During tear-down, the controller
// will call quiesceWork() which will release these workers and let them exit.
//
type state struct {
	mu sync.Mutex

	// indicates that new work is available. This wakes up any sleeping go routines waiting in dequeue.
	available *sync.Cond

	// Indicates that we should start reconciling changes to the CRD status. We don't want immediate reconciliation:
	// Galley may not have finished processing all resources while  existing status messages start trickling in.
	// Wait until the first set of diagnostic messages arrive (which maybe empty) before starting the reconciling of
	// status fields.
	reconcile bool

	// quiesce work. Go routine(s) that are blocked on dequeue will wake up and exit.
	quiesce bool

	// Keeps track of status for each resource. To reduce memory pressure, this will not contain entries for resources
	// that has both its desired and actual status empty.
	states map[key]*status

	// linked list implementation for the work queue. We're not using channels intentionally, as channel size is fixed
	// which can potentially cause unexpected blocking throughout the system.
	head *status
	tail *status
}

func newState() *state {
	s := &state{
		states: make(map[key]*status),
	}
	s.available = sync.NewCond(&s.mu)

	return s
}

// set the last observed state of a resource status, based on the watch events. This can trigger creation of new
// work, if the state is not as expected.
func (s *state) setObserved(col collection.Name, res resource.FullName, version resource.Version, status interface{}) {
	s.mu.Lock()
	k := key{col: col, res: res}

	st := s.states[k]

	if status == nil && st == nil {
		// The incoming state is empty. We only need to take action if the desired state we have is non-empty.
		// If the state doesn't exist, it implies that there is no desired state, so we can simply ignore this.
		s.mu.Unlock()
		return
	}

	// There is no known state. get a status from pool and add it to the mapping.
	if st == nil {
		st = getStatusFromPool(k)
		s.states[k] = st
	}

	// Set the last known status for this resource. If this causes a need for update, then enqueue work for thw workers.
	if st.setObserved(version, status) {
		if s.reconcile {
			s.enqueueWork(st)
		}
	} else if st.isEmpty() && !st.isEnqueued() {
		// The state is empty (and we want it to be empty), and we don't have this status enqueued in the work queue.
		// We can simply remove it and stop tracking.
		delete(s.states, k)
		returnStatusToPool(st)
	}

	s.mu.Unlock()
}

// Apply the given set of messages to their target resources.
func (s *state) applyMessages(messages Messages) {
	s.mu.Lock()
	// Start processing if we haven't already started to do so.
	s.reconcile = true

	// First, iterate through existing states and try to apply the message to that.
	for k, st := range s.states {
		e := messages.entries[k]

		if len(e.messages) > 0 {
			st.setDesired(e.origin.Version, toStatusValue(e.messages))
			// We applied the state and this caused a need for change. Enqueue work.
			s.enqueueWork(st)
		} else {
			// The desired state for the resource is empty.

			// Ignoring the return value, as the state should always change. Otherwise, the e would have been
			// removed when it was previously set to nil.
			_ = st.setDesired("", nil)
			s.enqueueWork(st)
		}

		// Delete the processed message.
		delete(messages.entries, k)
	}

	// Iterate over messages that didn't have an existing status and create new status & work.
	for k, e := range messages.entries {
		if len(e.messages) == 0 {
			continue
		}

		st := getStatusFromPool(k)
		s.states[k] = st

		_ = st.setDesired(e.origin.Version, toStatusValue(e.messages))
		s.enqueueWork(st)
	}

	s.mu.Unlock()
}

func (s *state) enqueueWork(st *status) {
	// must be called under lock

	if st.next != nil {
		// this status is already enqueued for work. Nothing to do.
		return
	}

	if s.head == nil {
		s.head = st
	} else {
		s.tail.next = st
	}
	// use sentinel value to tag work items that are in queue. This way, we can immediately tell if an item is enqueued
	// for work or not by looking at st.next.
	st.next = sentinel

	s.tail = st

	s.available.Broadcast()
}

// dequeue work and return it. If the dequeue was successful, then it returns a copy of the status and true. If
// the state is asked to quiesce work, it will return false. Otherwise, the call will be blocked until new work arrives.
func (s *state) dequeueWork() (status, bool) {
	s.mu.Lock()

	for {
		if s.quiesce {
			s.mu.Unlock()
			return status{}, false
		}

		if s.head != nil {
			st := s.head
			s.head = st.next
			if s.head == sentinel {
				s.head = nil
				s.tail = nil
			}
			st.next = nil

			if st.needsChange() {
				s.mu.Unlock()
				return *st, true
			}

			if st.isEmpty() {
				delete(s.states, st.key)
				returnStatusToPool(st)
			}
			continue
		}

		s.available.Wait()
	}
}

func (s *state) quiesceWork() {
	s.mu.Lock()
	s.quiesce = true
	s.available.Broadcast()
	s.mu.Unlock()
}

func (s *state) hasWork() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.head != nil
}
