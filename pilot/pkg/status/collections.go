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

// Package ingress provides a read-only view of Kubernetes ingress resources
// as an ingress rule configuration type store

package status

import (
	"strconv"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
)

type StatusRegistration = func(statusWriter Queue) krt.HandlerRegistration

// StatusCollections stores a variety of collections that can write status.
// These can be fed into a queue which can be dynamically changed (to handle leader election)
type StatusCollections struct {
	mu           sync.Mutex
	constructors []func(statusWriter Queue) krt.HandlerRegistration
	active       []krt.HandlerRegistration
	queue        Queue
}

func (s *StatusCollections) Register(sr StatusRegistration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.constructors = append(s.constructors, sr)
}

func (s *StatusCollections) UnsetQueue() {
	// Now we are disabled
	s.queue = nil
	for _, act := range s.active {
		act.UnregisterHandler()
	}
	s.active = nil
}

func (s *StatusCollections) SetQueue(queue Queue) []krt.Syncer {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Now we are enabled!
	s.queue = queue
	// Register all constructors
	s.active = slices.Map(s.constructors, func(reg StatusRegistration) krt.HandlerRegistration {
		return reg(queue)
	})
	return slices.Map(s.active, func(e krt.HandlerRegistration) krt.Syncer {
		return e
	})
}

// RegisterStatus takes a status collection and registers it to be managed by the status queue.
// krt.ObjectWithStatus, in theory, can contain anything in the "object" field. This function requires it to contain
// the current live *status*, and a passed in getStatus to extract it from the object.
// It will then compare the live status to the desired status to determine whether to write or not.
// Status is also reconciled when this revision gains ownership through a tag update. Ownership lives
// in the status registration so that a tag change can generate a status event even when the resource,
// desired status and config are identical.
func RegisterStatus[I controllers.Object, IS any](
	s *StatusCollections,
	statusCol krt.StatusCollection[I, IS],
	getStatus func(I) IS,
	tagWatcher krt.RecomputeProtected[revisions.TagWatcher],
	opts ...krt.CollectionOption,
) {
	withOwnership := krt.NewCollection(statusCol, func(ctx krt.HandlerContext, o krt.ObjectWithStatus[I, IS]) *statusWithOwnership[I, IS] {
		return &statusWithOwnership[I, IS]{
			ObjectWithStatus: o,
			Owned:            ownsStatus(tagWatcher.Get(ctx), o.Obj),
		}
	}, opts...)
	s.Register(func(statusWriter Queue) krt.HandlerRegistration {
		return withOwnership.Register(func(o krt.Event[statusWithOwnership[I, IS]]) {
			latest := o.Latest()
			if !latest.Owned {
				return
			}
			reconcileStatus(statusWriter, latest.ObjectWithStatus, o.Event, getStatus, tagWatcher.AccessUnprotected())
		})
	})
}

type statusWithOwnership[I controllers.Object, IS any] struct {
	krt.ObjectWithStatus[I, IS]
	Owned bool
}

func (s statusWithOwnership[I, IS]) Equals(other statusWithOwnership[I, IS]) bool {
	return s.Owned == other.Owned && s.ObjectWithStatus.Equals(other.ObjectWithStatus)
}

func reconcileStatus[I controllers.Object, IS any](
	statusWriter Queue,
	desired krt.ObjectWithStatus[I, IS],
	event controllers.EventType,
	getStatus func(I) IS,
	tagWatcher revisions.TagWatcher,
) {
	if krt.Equal(getStatus(desired.Obj), desired.Status) {
		// We want the same status we already have! No need for a write so skip this.
		// Note: the Equals() function on ObjectWithStatus does not compare these. It only compares "old live + desired" == "new live + desired".
		// So if either the live OR the desired status changes, this callback will trigger.
		// Here, we do smarter filtering and can just check whether we meet the desired state.
		log.Debugf("suppress change for %v %v", desired.ResourceName(), desired.Obj.GetResourceVersion())
		return
	}
	if !ownsStatus(tagWatcher, desired.Obj) {
		log.Debugf("suppress change for %v %v because it does not belong to my revision", desired.ResourceName(), desired.Obj.GetResourceVersion())
		return
	}
	wanted := &desired.Status
	if event == controllers.EventDelete {
		// if the object is being deleted, we should not reset status
		var empty IS
		wanted = &empty
	}
	enqueueStatus(statusWriter, desired.Obj, wanted)
	log.Debugf("Enqueued status update for %v %v: %v", desired.ResourceName(), desired.Obj.GetResourceVersion(), wanted)
}

func ownsStatus(watcher revisions.TagWatcher, obj controllers.Object) bool {
	return watcher.IsMine(metav1.ObjectMeta{Namespace: obj.GetNamespace(), Name: obj.GetName(), Labels: obj.GetLabels()})
}

func enqueueStatus[T any](sw Queue, obj controllers.Object, ws T) {
	// TODO: this is a bit awkward since the status controller is reading from crdstore. I suppose it works -- it just means
	// we cannot remove Gateway API types from there.
	res := Resource{
		GroupVersionResource: schematypes.GvrFromObject(obj),
		Namespace:            obj.GetNamespace(),
		Name:                 obj.GetName(),
		Generation:           strconv.FormatInt(obj.GetGeneration(), 10),
	}
	sw.EnqueueStatusUpdateResource(ws, res)
}
