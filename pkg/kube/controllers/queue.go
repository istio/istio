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

type Queue struct {
	queue       workqueue.RateLimitingInterface
	initialSync *atomic.Bool
	name        string
	maxAttempts int
	work        func(key interface{}) error
}

func WithName(name string) func(q *Queue) {
	return func(q *Queue) {
		q.name = name
	}
}

func WithRateLimiter(r workqueue.RateLimiter) func(q *Queue) {
	return func(q *Queue) {
		q.queue = workqueue.NewRateLimitingQueue(r)
	}
}

func WithMaxAttempts(n int) func(q *Queue) {
	return func(q *Queue) {
		q.maxAttempts = n
	}
}

func WithReconciler(f func(name types.NamespacedName) error) func(q *Queue) {
	return func(q *Queue) {
		q.work = func(key interface{}) error {
			return f(key.(types.NamespacedName))
		}
	}
}

func WithWork(f func(key interface{}) error) func(q *Queue) {
	return func(q *Queue) {
		q.work = f
	}
}

type syncSignal struct{}

var defaultSyncSignal = syncSignal{}

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

func (q Queue) Add(item interface{}) {
	q.queue.Add(item)
}

func (q Queue) Run(stop <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer q.queue.ShutDown()
	log.Infof("starting %v", q.name)
	q.Add(defaultSyncSignal)
	go func() {
		// Process updates until we return false, which indicates the queue is terminated
		for q.processNextItem() {
		}
	}()
	<-stop
	log.Infof("stopped %v", q.name)
}

func (q Queue) HasSynced() bool {
	return q.initialSync.Load()
}

func (q Queue) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := q.queue.Get()
	if quit {
		return false
	}
	if key == defaultSyncSignal {
		log.Debugf("%v synced", q.name)
		q.initialSync.Store(true)
		return true
	}

	log.Debugf("handling update for %v: %v", q.name, key)

	defer q.queue.Done(key)

	err := q.work(key)
	if err != nil {
		if q.queue.NumRequeues(key) < q.maxAttempts {
			log.Errorf("%v: error handling %v, retrying: %v", q.name, key, err)
			q.queue.AddRateLimited(key)
			return true
		} else {
			log.Errorf("error handling %v, and retry budget exceeded: %v", key, err)
		}
	}
	q.queue.Forget(key)
	return true
}

func (q Queue) AddObject(obj Object) {
	q.Add(types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	})
}
