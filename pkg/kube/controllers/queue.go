package controllers

import (
	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/workqueue"
)

type Enqueuer interface {
	Add(item interface{})
}

// Queue defines an abstraction around Kubernetes' workqueue.
// Items enqueued are deduplicated; this generally means relying on ordering of events in the queue is not feasible.
type Queue struct {
	queue       workqueue.RateLimitingInterface
	initialSync *atomic.Bool
	name        string
	maxAttempts int
	work        func(key interface{}) error
}

// WithName sets a name for the queue. This is used for logging
func WithName(name string) func(q *Queue) {
	return func(q *Queue) {
		q.name = name
	}
}

// WithRateLimiter allows defining a custom rate limitter for the queue
func WithRateLimiter(r workqueue.RateLimiter) func(q *Queue) {
	return func(q *Queue) {
		q.queue = workqueue.NewRateLimitingQueue(r)
	}
}

// WithMaxAttempts allows defining a custom max attempts for the queue. If not set, items will be discarded after 5 attempts
func WithMaxAttempts(n int) func(q *Queue) {
	return func(q *Queue) {
		q.maxAttempts = n
	}
}

// WithReconciler defines the to handle items on the queue
func WithReconciler(f func(name types.NamespacedName) error) func(q *Queue) {
	return func(q *Queue) {
		q.work = func(key interface{}) error {
			return f(key.(types.NamespacedName))
		}
	}
}

// NewQueue creates a new queue
func NewQueue(options ...func(*Queue)) Queue {
	q := Queue{
		initialSync: atomic.NewBool(false),
	}
	for _, o := range options {
		o(&q)
	}
	if q.name == "" {
		q.name = "queue"
	}
	if q.queue == nil {
		q.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	}
	if q.maxAttempts == 0 {
		q.maxAttempts = 5
	}
	return q
}

// Add an item to the queue.
func (q Queue) Add(item interface{}) {
	q.queue.Add(item)
}

// AddObject takes an Object and adds the types.NamespacedName associated.
func (q Queue) AddObject(obj Object) {
	q.Add(types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	})
}

// Run the queue. This is synchronous, so should typically be called in a goroutine.
func (q Queue) Run(stop <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer q.queue.ShutDown()
	log.Infof("starting %v", q.name)
	q.Add(defaultSyncSignal)
	done := make(chan struct{})
	go func() {
		// Process updates until we return false, which indicates the queue is terminated
		for q.processNextItem() {
		}
		close(done)
	}()
	select {
	case <-stop:
	case <-done:
	}
	log.Infof("stopped %v", q.name)
}

// syncSignal defines a dummy signal that is enqueued when .Run() is called. This allows us to detect
// when we have processed all items added to the queue prior to Run().
type syncSignal struct{}

var defaultSyncSignal = syncSignal{}

// HasSynced returns true if the queue has 'synced'. A synced queue has started running and has
// processed all events that were added prior to Run() being called Warning: these items will be
// processed at least once, but may have failed.
func (q Queue) HasSynced() bool {
	return q.initialSync.Load()
}

// processNextItem is the main work loop for the queue
func (q Queue) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := q.queue.Get()
	if quit {
		// We are done, signal to exit the queue
		return false
	}

	// We got the sync signal. This is not a real event, so we exit early after signaling we are synced
	if key == defaultSyncSignal {
		log.Debugf("%v synced", q.name)
		q.initialSync.Store(true)
		return true
	}

	log.Debugf("handling update for %v: %v", q.name, key)

	// 'Done marks item as done processing' - should be called at the end of all processing
	defer q.queue.Done(key)

	err := q.work(key)
	if err != nil {
		if q.queue.NumRequeues(key) < q.maxAttempts {
			log.Errorf("%v: error handling %v, retrying: %v", q.name, key, err)
			q.queue.AddRateLimited(key)
			// Return early, so we do not call Forget(), allowing the rate limiting to backoff
			return true
		} else {
			log.Errorf("error handling %v, and retry budget exceeded: %v", key, err)
		}
	}
	// 'Forget indicates that an item is finished being retried.' - should be called whenever we do not want to backoff on this key.
	q.queue.Forget(key)
	return true
}
