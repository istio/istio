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

package statusqueue

import (
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	istiolog "istio.io/istio/pkg/log"
)

var log = istiolog.RegisterScope("status", "status reporting")

type StatusQueue struct {
	queue controllers.Queue

	// reporters is a mapping of unique controller name -> status information.
	// Note: this is user facing in the fieldManager!
	reporters map[string]statusReporter

	// statusEnabled keeps track if we are currently statusEnabled status. This is set to 'true' when we are currently the leader.
	// This is awkward in order to plumb through the leader status from the top of the process down to this controller.
	// We want to meet these requirements:
	// * When we start leading, we MUST process all existing items (and then any updates of course)
	// * When we stop leading, we MUST not write status any longer
	// * When we stop leading, we SHOULD do as little work as possible
	// To do this, we keep track of an ActiveNotifier which stores the current state (leading or not) and notifies us on changes.
	statusEnabled *activenotifier.ActiveNotifier
}

// statusItem represents the objects stored on the queue
type statusItem struct {
	Key      string
	Reporter string
}

// NewQueue builds a new status queue. ActiveNotifier must be provided.
func NewQueue(statusEnabled *activenotifier.ActiveNotifier) *StatusQueue {
	sq := &StatusQueue{
		reporters:     make(map[string]statusReporter),
		statusEnabled: statusEnabled,
	}
	statusEnabled.AddHandler(func(active bool) {
		if active {
			// If we are set to active, tell all the reporters to re-list the current state
			// This ensures we process any existing objects when we become the leader.
			for _, r := range sq.reporters {
				r.relist()
			}
		}
	})
	sq.queue = controllers.NewQueue("ambient status",
		controllers.WithGenericReconciler(sq.reconcile),
		controllers.WithMaxAttempts(5))
	return sq
}

// Run starts the queue, which will process items until the channel is closed
func (q *StatusQueue) Run(stop <-chan struct{}) {
	for _, r := range q.reporters {
		r.start()
	}
	q.queue.Run(stop)
}

// reconcile processes a single queue item
func (q *StatusQueue) reconcile(raw any) error {
	key := raw.(statusItem)
	log := log.WithLabels("key", key.Key)
	log.Debugf("reconciling status")

	reporter, f := q.reporters[key.Reporter]
	if !f {
		log.Fatalf("impossible; an item was enqueued with an unknown reporter")
	}
	obj, f := reporter.getObject(key.Key)
	if !f {
		log.Infof("object is removed, no action needed")
		return nil
	}
	// Fetch the client to apply patches, and the set of current conditions
	patcher, currentConditions := reporter.patcher(obj)
	// Turn the conditions into a patch. Using currentConditions, this will determine whether we can skip the patch entirely
	// or if we need to send an empty patch. With an empty patch, SSA will automatically prune out anything *we* (identified by the fieldManager) wrote.
	//
	// This gives us essentially the same behavior as always writing, but saves a lot of writes... with the caveat of one race condition:
	// the object can change between when we fetch the currentConditions and apply the patch.
	// * Condition was there, but is now removed: No problem, we will at worst do a patch that wasn't needed.
	// * Condition was not there, but now it was added: clearly some other controller is writing the same type as us, which is not really allowed.
	targetObject := obj.GetStatusTarget()
	status := translateToPatch(targetObject, obj.GetConditions(), currentConditions)

	if status == nil {
		log.Debugf("no status to write")
		return nil
	}
	log.Debugf("writing patch %v", string(status))
	// Pass key.Reporter as the fieldManager. This ensures we have a unique value there.
	// This means we could have multiple unique writers for the same object, as long as they have a unique set of conditions.
	return patcher.ApplyStatus(targetObject.Name, targetObject.Namespace, types.ApplyPatchType, status, key.Reporter)
}

// StatusWriter is a type that can write status messages
type StatusWriter interface {
	// GetStatusTarget returns the metadata about the object we are writing to
	GetStatusTarget() model.TypedObject
	// GetConditions returns a set of conditions for the object
	GetConditions() model.ConditionSet
}

// statusReporter is a generics-erased object storing context on how to write status for a given type.
type statusReporter struct {
	getObject func(string) (StatusWriter, bool)
	patcher   func(StatusWriter) (kclient.Patcher, map[string]model.Condition)
	relist    func()
	start     func()
}

// Register registers a collection to have status reconciled.
// The Collection is expected to produce objects that implement StatusWriter, which tells us what status to write.
// The name is user facing, and ends up as a fieldManager for server-side-apply. It must be unique.
func Register[T StatusWriter](q *StatusQueue, name string, col krt.Collection[T], getPatcher func(T) (kclient.Patcher, map[string]model.Condition)) {
	sr := statusReporter{
		getObject: func(s string) (StatusWriter, bool) {
			if o := col.GetKey(s); o != nil {
				return *o, true
			}
			return nil, false
		},
		// Wrapper to remove generics
		patcher: func(writer StatusWriter) (kclient.Patcher, map[string]model.Condition) {
			return getPatcher(writer.(T))
		},
		relist: func() {
			log.Debugf("processing snapshot")
			for _, obj := range col.List() {
				q.queue.Add(statusItem{
					Key:      krt.GetKey(obj),
					Reporter: name,
				})
			}
		},
		start: func() {
			col.RegisterBatch(func(events []krt.Event[T]) {
				if !q.statusEnabled.CurrentlyActive() {
					return
				}
				for _, o := range events {
					ol := o.Latest()
					key := krt.GetKey(ol)
					log.Debugf("registering key for processing: %s", key)
					q.queue.Add(statusItem{
						Key:      key,
						Reporter: name,
					})
				}
			}, true)
		},
	}
	q.reporters[name] = sr
}
