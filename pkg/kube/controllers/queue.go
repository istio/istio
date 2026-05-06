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

package controllers

import (
	"fmt"
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/config"
	istiolog "istio.io/istio/pkg/log"
)

type ReconcilerFn func(key types.NamespacedName) error

// Queue defines an abstraction around Kubernetes' workqueue.
// Items enqueued are deduplicated; this generally means relying on ordering of events in the queue is not feasible.
type Queue struct {
	queue       workqueue.TypedRateLimitingInterface[any]
	initialSync *atomic.Bool
	name        string
	maxAttempts int
	workFn      func(key any) error
	closed      chan struct{}
	log         *istiolog.Scope
}

// WithName sets a name for the queue. This is used for logging
func WithName(name string) func(q *Queue) {
	return func(q *Queue) {
		q.name = name
	}
}

// WithRateLimiter allows defining a custom rate limiter for the queue
func WithRateLimiter(r workqueue.TypedRateLimiter[any]) func(q *Queue) {
	return func(q *Queue) {
		q.queue = workqueue.NewTypedRateLimitingQueue[any](r)
	}
}

// WithMaxAttempts allows defining a custom max attempts for the queue. If not set, items will not be retried
func WithMaxAttempts(n int) func(q *Queue) {
	return func(q *Queue) {
		q.maxAttempts = n
	}
}

// WithReconciler defines the handler function to handle items in the queue.
func WithReconciler(f ReconcilerFn) func(q *Queue) {
	return func(q *Queue) {
		q.workFn = func(key any) error {
			return f(key.(types.NamespacedName))
		}
	}
}

// WithGenericReconciler defines the handler function to handle items in the queue that can handle any type
func WithGenericReconciler(f func(key any) error) func(q *Queue) {
	return func(q *Queue) {
		q.workFn = func(key any) error {
			return f(key)
		}
	}
}

// NewQueue creates a new queue
func NewQueue(name string, options ...func(*Queue)) Queue {
	q := Queue{
		name:        name,
		closed:      make(chan struct{}),
		initialSync: atomic.NewBool(false),
	}
	for _, o := range options {
		o(&q)
	}
	if q.queue == nil {
		q.queue = workqueue.NewTypedRateLimitingQueueWithConfig[any](
			workqueue.DefaultTypedControllerRateLimiter[any](),
			workqueue.TypedRateLimitingQueueConfig[any]{
				Name:            name,
				MetricsProvider: nil,
				Clock:           nil,
				DelayingQueue:   nil,
			},
		)
	}
	q.log = log.WithLabels("controller", q.name)
	return q
}

// Add an item to the queue.
func (q Queue) Add(item any) {
	q.queue.Add(item)
}

// AddObject takes an Object and adds the types.NamespacedName associated.
func (q Queue) AddObject(obj Object) {
	q.queue.Add(config.NamespacedName(obj))
}

// ShutDownEarly shuts down the queue *before* it has been Run.
// Creating a queue without running it causes a leak, so this must be called on any queue that is closed without
func (q Queue) ShutDownEarly() {
	q.log.Infof("shutdown early")
	q.queue.ShutDown()
}

// Run the queue. This is synchronous, so should typically be called in a goroutine.
func (q Queue) Run(stop <-chan struct{}) {
	defer q.queue.ShutDown()
	q.log.Infof("starting")
	q.queue.Add(defaultSyncSignal)
	go func() {
		// Process updates until we return false, which indicates the queue is terminated
		for q.processNextItem() {
		}
		close(q.closed)
	}()
	select {
	case <-stop:
	case <-q.closed:
	}
	q.log.Infof("stopped")
}

// syncSignal defines a dummy signal that is enqueued when .Run() is called. This allows us to detect
// when we have processed all items added to the queue prior to Run().
type syncSignal struct{}

// defaultSyncSignal is a singleton instanceof syncSignal.
var defaultSyncSignal = syncSignal{}

// HasSynced returns true if the queue has 'synced'. A synced queue has started running and has
// processed all events that were added prior to Run() being called Warning: these items will be
// processed at least once, but may have failed.
func (q Queue) HasSynced() bool {
	return q.initialSync.Load()
}

// Closed returns a chan that will be signaled when the Instance has stopped processing tasks.
func (q Queue) Closed() <-chan struct{} {
	return q.closed
}

// processNextItem is the main workFn loop for the queue
func (q Queue) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := q.queue.Get()
	if quit {
		// We are done, signal to exit the queue
		return false
	}

	// We got the sync signal. This is not a real event, so we exit early after signaling we are synced
	if key == defaultSyncSignal {
		q.log.Debugf("synced")
		q.initialSync.Store(true)
		return true
	}

	q.log.Debugf("handling update: %v", formatKey(key))

	// 'Done marks item as done processing' - should be called at the end of all processing
	defer q.queue.Done(key)

	err := q.workFn(key)
	if err != nil {
		retryCount := q.queue.NumRequeues(key) + 1
		if retryCount < q.maxAttempts {
			q.log.Errorf("error handling %v, retrying (retry count: %d): %v", formatKey(key), retryCount, err)
			q.queue.AddRateLimited(key)
			// Return early, so we do not call Forget(), allowing the rate limiting to backoff
			return true
		}
		q.log.Errorf("error handling %v, and retry budget exceeded: %v", formatKey(key), err)
	}
	// 'Forget indicates that an item is finished being retried.' - should be called whenever we do not want to backoff on this key.
	q.queue.Forget(key)
	return true
}

// WaitForClose blocks until the Instance has stopped processing tasks or the timeout expires.
// If the timeout is zero, it will wait until the queue is done processing.
// WaitForClose an error if the timeout expires.
func (q Queue) WaitForClose(timeout time.Duration) error {
	closed := q.Closed()
	if timeout == 0 {
		<-closed
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout waiting for queue to close after %v", timeout)
	}
}

func formatKey(key any) string {
	if t, ok := key.(types.NamespacedName); ok {
		if len(t.Namespace) > 0 {
			return t.String()
		}
		// because we use namespacedName for non namespace scope resource as well
		return t.Name
	}
	if t, ok := key.(Event); ok {
		key = t.Latest()
	}
	if t, ok := key.(Object); ok {
		if len(t.GetNamespace()) > 0 {
			return t.GetNamespace() + "/" + t.GetName()
		}
		return t.GetName()
	}
	res := fmt.Sprint(key)
	if len(res) >= 50 {
		return res[:50]
	}
	return res
}
