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

package krt

import (
	"sync"
	"sync/atomic"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/buffer"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type handlerRegistration struct {
	Syncer
	remove func()
}

func (h handlerRegistration) UnregisterHandler() {
	h.remove()
}

// handlerSet tracks a set of handlers. Handlers can be added at any time.
type handlerSet[O any] struct {
	mu       sync.RWMutex
	handlers sets.Set[*processorListener[O]]
	wg       wait.Group
}

func newHandlerSet[O any]() *handlerSet[O] {
	return &handlerSet[O]{
		handlers: sets.New[*processorListener[O]](),
	}
}

func (o *handlerSet[O]) Insert(
	f func(o []Event[O]),
	parentSynced Syncer,
	initialEvents []Event[O],
	stopCh <-chan struct{},
) HandlerRegistration {
	o.mu.Lock()
	initialSynced := parentSynced.HasSynced()
	l := newProcessListener(f, parentSynced, stopCh)
	o.handlers.Insert(l)
	o.wg.Start(l.run)
	o.wg.Start(l.pop)
	var sendSynced bool
	if initialSynced {
		// If we are already synced, and there are no events to handle, we should mark ourselves synced right away.
		if len(initialEvents) == 0 {
			l.syncTracker.ParentSynced()
		} else {
			// Otherwise, queue up a 'synced' event after we process the initial state
			sendSynced = true
		}
	} else {
		o.wg.Start(func() {
			// If we didn't start synced, register a callback to mark ourselves synced once the parent is synced.
			if parentSynced.WaitUntilSynced(stopCh) {
				o.mu.RLock()
				defer o.mu.RUnlock()
				if !o.handlers.Contains(l) {
					return
				}

				select {
				case <-l.stop:
					return
				case l.addCh <- parentSyncedNotification{}:
				}
			}
		})
	}
	o.mu.Unlock()
	l.send(initialEvents, true)
	if sendSynced {
		l.addCh <- parentSyncedNotification{}
	}
	reg := handlerRegistration{
		Syncer: l.Synced(),
		remove: func() {
			o.remove(l)
		},
	}
	return reg
}

func (o *handlerSet[O]) remove(p *processorListener[O]) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delete(o.handlers, p)
	close(p.addCh)
}

func (o *handlerSet[O]) Distribute(events []Event[O], initialSync bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	for listener := range o.handlers {
		listener.send(slices.Clone(events), initialSync)
	}
}

// Synced returns a Syncer that will wait for all registered handlers (at the time of the call!) are synced.
func (o *handlerSet[O]) Synced() Syncer {
	o.mu.RLock()
	syncer := multiSyncer{syncers: make([]Syncer, 0, len(o.handlers))}
	for listener := range o.handlers {
		syncer.syncers = append(syncer.syncers, listener.Synced())
	}
	o.mu.RUnlock()
	return syncer
}

// processorListener is a fork of the upstream Kubernetes one. It has minimal tweaks to fit into
// our use case better, but the most important one being that processorListener is private in Kubernetes.
//
// processorListener relays notifications from a sharedProcessor to
// one ResourceEventHandler --- using two goroutines, two unbuffered
// channels, and an unbounded ring buffer.  The `add(notification)`
// function sends the given notification to `addCh`.  One goroutine
// runs `pop()`, which pumps notifications from `addCh` to `nextCh`
// using storage in the ring buffer while `nextCh` is not keeping up.
// Another goroutine runs `run()`, which receives notifications from
// `nextCh` and synchronously invokes the appropriate handler method.
//
// processorListener also keeps track of the adjusted requested resync
// period of the listener.
//
// Unlike the Kubernetes library, the syncTracker has a harder job in our usage. In Kubernetes, the initial state is
// always handled in a single call. However, in our usage we may get an initial state, then some later events, THEN be marked as synced.
type processorListener[O any] struct {
	nextCh chan any
	addCh  chan any
	stop   <-chan struct{}

	handler func(o []Event[O])

	syncTracker *countingTracker

	// pendingNotifications is an unbounded ring buffer that holds all notifications not yet distributed.
	// There is one per listener, but a failing/stalled listener will have infinite pendingNotifications
	// added until we OOM.
	// TODO: This is no worse than before, since reflectors were backed by unbounded DeltaFIFOs, but
	// we should try to do something better.
	pendingNotifications buffer.RingGrowing
}

func newProcessListener[O any](
	handler func(o []Event[O]),
	upstreamSyncer Syncer,
	stop <-chan struct{},
) *processorListener[O] {
	bufferSize := 1024
	ret := &processorListener[O]{
		nextCh:               make(chan any),
		addCh:                make(chan any),
		stop:                 stop,
		handler:              handler,
		syncTracker:          &countingTracker{upstreamSyncer: upstreamSyncer, synced: make(chan struct{})},
		pendingNotifications: *buffer.NewRingGrowing(bufferSize),
	}

	return ret
}

type eventSet[O any] struct {
	event           []Event[O]
	isInInitialList bool
}

func (p *processorListener[O]) send(event []Event[O], isInInitialList bool) {
	if isInInitialList {
		// Mark how many items we have left to process
		p.syncTracker.Start(len(event))
	}
	select {
	case <-p.stop:
		return
	case p.addCh <- eventSet[O]{event: event, isInInitialList: isInInitialList}:
	}
}

func (p *processorListener[O]) pop() {
	defer utilruntime.HandleCrash()
	defer close(p.nextCh) // Tell .run() to stop

	var nextCh chan<- any
	var notification any
	for {
		select {
		case <-p.stop:
			return
		case nextCh <- notification:
			// Notification dispatched
			var ok bool
			notification, ok = p.pendingNotifications.ReadOne()
			if !ok { // Nothing to pop
				nextCh = nil // Disable this select case
			}
		case notificationToAdd, ok := <-p.addCh:
			if !ok {
				return
			}
			if notification == nil { // No notification to pop (and pendingNotifications is empty)
				// Optimize the case - skip adding to pendingNotifications
				notification = notificationToAdd
				nextCh = p.nextCh
			} else { // There is already a notification waiting to be dispatched
				p.pendingNotifications.WriteOne(notificationToAdd)
			}
		}
	}
}

// parentSyncedNotification is a signal to indicate the parent has synced.
// This is sent over the nextCh to ensure ordering and single-threaded usage
type parentSyncedNotification struct{}

func (p *processorListener[O]) run() {
	for {
		select {
		case <-p.stop:
			return
		case nextr, ok := <-p.nextCh:
			if !ok {
				return
			}
			if _, ok := nextr.(parentSyncedNotification); ok {
				p.syncTracker.ParentSynced()
				continue
			}
			next := nextr.(eventSet[O])
			if !next.isInInitialList {
				// If we got an event outside the initial list, we definitely have the parent synced.
				// This can happen due to a race, where we get a non-initial event before we get the 'parent synced' notification,
				// which is handled on a separate thread.
				// In rare cases, the non-initial event could block, so its optimal to mark ourselves as synced before, rather than after,
				// processing it.
				p.syncTracker.ParentSynced()
			}
			if len(next.event) > 0 {
				p.handler(next.event)
			}
			if next.isInInitialList {
				p.syncTracker.Finished(len(next.event))
			}
		}
	}
}

func (p *processorListener[O]) Synced() Syncer {
	return p.syncTracker.Synced()
}

// countingTracker helps propagate HasSynced when events are processed in
// order (i.e. via a queue).
type countingTracker struct {
	count int64

	// upstreamHasSyncedButEventsPending marks true if the parent has synced, but there are still events pending
	// This helps us known when we need to mark ourselves as 'synced' (which we do exactly once).
	upstreamHasSyncedButEventsPending bool
	upstreamSyncer                    Syncer
	synced                            chan struct{}
	hasSynced                         bool
}

// Start should be called prior to processing each key which is part of the
// initial list.
func (t *countingTracker) Start(count int) {
	atomic.AddInt64(&t.count, int64(count))
}

func (t *countingTracker) ParentSynced() {
	if t.hasSynced {
		// Already synced, no change needed
		return
	}
	if atomic.LoadInt64(&t.count) == 0 {
		// No pending events, so we are synced
		close(t.synced)
		t.hasSynced = true
	} else {
		// Else, indicate we should be synced as soon as we process the remainder of events
		t.upstreamHasSyncedButEventsPending = true
	}
}

// Finished should be called when finished processing a key which was part of
// the initial list. You must never call Finished() before (or without) its
// corresponding Start(), that is a logic error that could cause HasSynced to
// return a wrong value. To help you notice this should it happen, Finished()
// will panic if the internal counter goes negative.
func (t *countingTracker) Finished(count int) {
	result := atomic.AddInt64(&t.count, -int64(count))
	if result < 0 {
		panic("synctrack: negative counter; this logic error means HasSynced may return incorrect value")
	}
	if !t.hasSynced && t.upstreamHasSyncedButEventsPending && result == 0 && count != 0 {
		close(t.synced)
	}
}

// Synced returns a syncer that responds as "Synced" when the underlying collection is synced, and the
// initial list has been processed. This relies on the source not considering
// itself synced until *after* it has delivered the notification for the last
// key, and that notification handler must have called Start.
func (t *countingTracker) Synced() Syncer {
	// Call UpstreamHasSynced first: it might take a lock, which might take
	// a significant amount of time, and we don't want to then act on a
	// stale count value.
	return multiSyncer{
		syncers: []Syncer{
			t.upstreamSyncer,
			channelSyncer{synced: t.synced, name: "tracker"},
		},
	}
}
