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

	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
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

// registerStatus takes a status collection and registers it to be managed by the status queue.
func RegisterStatus[I controllers.Object, IS any](s *StatusCollections, statusCol krt.StatusCollection[I, IS]) {
	reg := func(statusWriter Queue) krt.HandlerRegistration {
		h := statusCol.Register(func(o krt.Event[krt.ObjectWithStatus[I, IS]]) {
			l := o.Latest()
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
	s.Register(reg)
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
