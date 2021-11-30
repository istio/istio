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

	"go.uber.org/atomic"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("controllers", "common controller logic", 0)

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

// Object is a union of runtime + meta objects. Essentially every k8s object meets this interface.
// and certainly all that we care about.
type Object interface {
	metav1.Object
	runtime.Object
}

// UnstructuredToGVR extracts the GVR of an unstructured resource. This is useful when using dynamic
// clients.
func UnstructuredToGVR(u unstructured.Unstructured) (schema.GroupVersionResource, error) {
	res := schema.GroupVersionResource{}
	gv, err := schema.ParseGroupVersion(u.GetAPIVersion())
	if err != nil {
		return res, err
	}

	gk := config.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    u.GetKind(),
	}
	found, ok := collections.All.FindByGroupVersionKind(gk)
	if !ok {
		return res, fmt.Errorf("unknown gvk: %v", gk)
	}
	return schema.GroupVersionResource{
		Group:    gk.Group,
		Version:  gk.Version,
		Resource: found.Resource().Plural(),
	}, nil
}

// ObjectToGVR extracts the GVR of an unstructured resource. This is useful when using dynamic
// clients.
func ObjectToGVR(u Object) (schema.GroupVersionResource, error) {
	kGvk := u.GetObjectKind().GroupVersionKind()

	gk := config.GroupVersionKind{
		Group:   kGvk.Group,
		Version: kGvk.Version,
		Kind:    kGvk.Kind,
	}
	found, ok := collections.All.FindByGroupVersionKind(gk)
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown gvk: %v", gk)
	}
	return schema.GroupVersionResource{
		Group:    gk.Group,
		Version:  gk.Version,
		Resource: found.Resource().Plural(),
	}, nil
}

// EnqueueForParentHandler returns a handler that will enqueue the parent (by ownerRef) resource
func EnqueueForParentHandler(q Enqueuer, kind config.GroupVersionKind) func(obj Object) {
	handler := func(obj Object) {
		for _, ref := range obj.GetOwnerReferences() {
			refGV, err := schema.ParseGroupVersion(ref.APIVersion)
			if err != nil {
				log.Errorf("could not parse OwnerReference api version %q: %v", ref.APIVersion, err)
				continue
			}
			if refGV == kind.Kubernetes().GroupVersion() {
				// We found a parent we care about, add it to the queue
				q.Add(types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      obj.GetName(),
				})
			}
		}
	}
	return handler
}

// EnqueueForSelf returns a handler that will add itself to the queue
func EnqueueForSelf(q Enqueuer) func(obj Object) {
	return func(obj Object) {
		q.Add(types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		})
	}
}

// LatestVersionHandlerFuncs returns a handler that will act on the latest version of an object
// This means Add/Update/Delete are all handled the same and are just used to trigger reconciling.
// If filters are set, returning 'false' will exclude the event
func LatestVersionHandlerFuncs(handler func(o Object), filters ...func(o Object) bool) cache.ResourceEventHandler {
	h := fromObjectHandler(handler, filters...)
	return cache.ResourceEventHandlerFuncs{
		AddFunc: h,
		UpdateFunc: func(oldObj, newObj interface{}) {
			h(newObj)
		},
		DeleteFunc: h,
	}
}

// fromObjectHandler takes in a handler for an Object and returns a handler for interface{}
// that can be passed to raw Kubernetes libraries.
func fromObjectHandler(handler func(o Object), filters ...func(o Object) bool) func(obj interface{}) {
	return func(obj interface{}) {
		o, ok := obj.(Object)
		if !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				log.Errorf("couldn't get object from tombstone %+v", obj)
				return
			}
			o, ok = tombstone.Obj.(Object)
			if !ok {
				log.Errorf("tombstone contained object that is not an object %+v", obj)
				return
			}
		}
		for _, f := range filters {
			if !f(o) {
				return
			}
		}
		handler(o)
	}
}

// IgnoreNotFound returns nil on NotFound errors.
// All other values that are not NotFound errors or nil are returned unmodified.
func IgnoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
