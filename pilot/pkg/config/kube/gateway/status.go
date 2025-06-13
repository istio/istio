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

package gateway

import (
	"strconv"
	"sync"

	"istio.io/istio/pilot/pkg/status"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
)

type StatusRegistration = func(statusWriter status.Queue) krt.HandlerRegistration

// StatusCollections stores a variety of collections that can write status.
// These can be fed into a queue which can be dynamically changed (to handle leader election)
type StatusCollections struct {
	mu           sync.Mutex
	constructors []func(statusWriter status.Queue) krt.HandlerRegistration
	active       []krt.HandlerRegistration
	queue        status.Queue
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

func (s *StatusCollections) SetQueue(queue status.Queue) []krt.Syncer {
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
func registerStatus[I controllers.Object, IS any](c *Controller, statusCol krt.StatusCollection[I, IS], getStatus func(I) IS) {
	reg := func(statusWriter status.Queue) krt.HandlerRegistration {
		h := statusCol.Register(func(o krt.Event[krt.ObjectWithStatus[I, IS]]) {
			l := o.Latest()
			liveStatus := getStatus(l.Obj)
			if krt.Equal(liveStatus, l.Status) {
				// We want the same status we already have! No need for a write so skip this.
				// Note: the Equals() function on ObjectWithStatus does not compare these. It only compares "old live + desired" == "new live + desired".
				// So if either the live OR the desired status changes, this callback will trigger.
				// Here, we do smarter filtering and can just check whether we meet the desired state.
				log.Debugf("suppress change for %v %v", l.ResourceName(), l.Obj.GetResourceVersion())
				return
			}
			status := &l.Status
			if o.Event == controllers.EventDelete {
				// if the object is being deleted, we should not reset status
				var empty IS
				status = &empty
			}
			enqueueStatus(statusWriter, l.Obj, status)
		})
		return h
	}
	c.status.Register(reg)
}

func enqueueStatus[T any](sw status.Queue, obj controllers.Object, ws T) {
	// TODO: this is a bit awkward since the status controller is reading from crdstore. I suppose it works -- it just means
	// we cannot remove Gateway API types from there.
	res := status.Resource{
		GroupVersionResource: schematypes.GvrFromObject(obj),
		Namespace:            obj.GetNamespace(),
		Name:                 obj.GetName(),
		Generation:           strconv.FormatInt(obj.GetGeneration(), 10),
	}
	sw.EnqueueStatusUpdateResource(ws, res)
}
