/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package task

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

var (
	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// Queue manages a work queue through an independent worker that
// invokes the given sync function for every work item inserted.
type Queue struct {
	// queue is the work queue the worker polls
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue
	sync func(interface{}) error
	// workerDone is closed when the worker exits
	workerDone chan bool

	fn func(obj interface{}) (interface{}, error)
}

// Run ...
func (t *Queue) Run(period time.Duration, stopCh <-chan struct{}) {
	wait.Until(t.worker, period, stopCh)
}

// Enqueue enqueues ns/name of the given api object in the task queue.
func (t *Queue) Enqueue(obj interface{}) {
	if t.IsShuttingDown() {
		glog.Errorf("queue has been shutdown, failed to enqueue: %v", obj)
		return
	}

	glog.V(3).Infof("queuing item %v", obj)
	key, err := t.fn(obj)
	if err != nil {
		glog.Errorf("%v", err)
		return
	}
	t.queue.Add(key)
}

func (t *Queue) defaultKeyFunc(obj interface{}) (interface{}, error) {
	key, err := keyFunc(obj)
	if err != nil {
		return "", fmt.Errorf("could not get key for object %+v: %v", obj, err)
	}

	return key, nil
}

// worker processes work in the queue through sync.
func (t *Queue) worker() {
	for {
		key, quit := t.queue.Get()
		if quit {
			if !isClosed(t.workerDone) {
				close(t.workerDone)
			}
			return
		}

		glog.V(3).Infof("syncing %v", key)
		if err := t.sync(key); err != nil {
			glog.Warningf("requeuing %v, err %v", key, err)
			t.queue.AddRateLimited(key)
		} else {
			t.queue.Forget(key)
		}

		t.queue.Done(key)
	}
}

func isClosed(ch <-chan bool) bool {
	select {
	case <-ch:
		return true
	default:
	}

	return false
}

// Shutdown shuts down the work queue and waits for the worker to ACK
func (t *Queue) Shutdown() {
	t.queue.ShutDown()
	<-t.workerDone
}

// IsShuttingDown returns if the method Shutdown was invoked
func (t *Queue) IsShuttingDown() bool {
	return t.queue.ShuttingDown()
}

// NewTaskQueue creates a new task queue with the given sync function.
// The sync function is called for every element inserted into the queue.
func NewTaskQueue(syncFn func(interface{}) error) *Queue {
	return NewCustomTaskQueue(syncFn, nil)
}

// NewCustomTaskQueue ...
func NewCustomTaskQueue(syncFn func(interface{}) error, fn func(interface{}) (interface{}, error)) *Queue {
	q := &Queue{
		queue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		sync:       syncFn,
		workerDone: make(chan bool),
		fn:         fn,
	}

	if fn == nil {
		q.fn = q.defaultKeyFunc
	}

	return q
}
