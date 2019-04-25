/*
Copyright 2018 The Kubernetes Authors.

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

package internal

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var log = logf.KBLog.WithName("source").WithName("EventHandler")

var _ cache.ResourceEventHandler = EventHandler{}

// EventHandler adapts a eventhandler.EventHandler interface to a cache.ResourceEventHandler interface
type EventHandler struct {
	EventHandler handler.EventHandler
	Queue        workqueue.RateLimitingInterface
	Predicates   []predicate.Predicate
}

// OnAdd creates and CreateEvent and calls Create on EventHandler
func (e EventHandler) OnAdd(obj interface{}) {
	c := event.CreateEvent{}

	// Pull metav1.Object out of the object
	if o, err := meta.Accessor(obj); err == nil {
		c.Meta = o
	} else {
		log.Error(err, "OnAdd missing Meta",
			"object", obj, "type", fmt.Sprintf("%T", obj))
		return
	}

	// Pull the runtime.Object out of the object
	if o, ok := obj.(runtime.Object); ok {
		c.Object = o
	} else {
		log.Error(nil, "OnAdd missing runtime.Object",
			"object", obj, "type", fmt.Sprintf("%T", obj))
		return
	}

	for _, p := range e.Predicates {
		if !p.Create(c) {
			return
		}
	}

	// Invoke create handler
	e.EventHandler.Create(c, e.Queue)
}

// OnUpdate creates and UpdateEvent and calls Update on EventHandler
func (e EventHandler) OnUpdate(oldObj, newObj interface{}) {
	u := event.UpdateEvent{}

	// Pull metav1.Object out of the object
	if o, err := meta.Accessor(oldObj); err == nil {
		u.MetaOld = o
	} else {
		log.Error(err, "OnUpdate missing MetaOld",
			"object", oldObj, "type", fmt.Sprintf("%T", oldObj))
		return
	}

	// Pull the runtime.Object out of the object
	if o, ok := oldObj.(runtime.Object); ok {
		u.ObjectOld = o
	} else {
		log.Error(nil, "OnUpdate missing ObjectOld",
			"object", oldObj, "type", fmt.Sprintf("%T", oldObj))
		return
	}

	// Pull metav1.Object out of the object
	if o, err := meta.Accessor(newObj); err == nil {
		u.MetaNew = o
	} else {
		log.Error(err, "OnUpdate missing MetaNew",
			"object", newObj, "type", fmt.Sprintf("%T", newObj))
		return
	}

	// Pull the runtime.Object out of the object
	if o, ok := newObj.(runtime.Object); ok {
		u.ObjectNew = o
	} else {
		log.Error(nil, "OnUpdate missing ObjectNew",
			"object", oldObj, "type", fmt.Sprintf("%T", oldObj))
		return
	}

	for _, p := range e.Predicates {
		if !p.Update(u) {
			return
		}
	}

	// Invoke update handler
	e.EventHandler.Update(u, e.Queue)
}

// OnDelete creates and DeleteEvent and calls Delete on EventHandler
func (e EventHandler) OnDelete(obj interface{}) {
	d := event.DeleteEvent{}

	// Deal with tombstone events by pulling the object out.  Tombstone events wrap the object in a
	// DeleteFinalStateUnknown struct, so the object needs to be pulled out.
	// Copied from sample-controller
	// This should never happen if we aren't missing events, which we have concluded that we are not
	// and made decisions off of this belief.  Maybe this shouldn't be here?
	var ok bool
	if _, ok = obj.(metav1.Object); !ok {
		// If the object doesn't have Metadata, assume it is a tombstone object of type DeletedFinalStateUnknown
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Error decoding objects.  Expected cache.DeletedFinalStateUnknown",
				"type", fmt.Sprintf("%T", obj),
				"object", obj)
			return
		}

		// Set obj to the tombstone obj
		obj = tombstone.Obj
	}

	// Pull metav1.Object out of the object
	if o, err := meta.Accessor(obj); err == nil {
		d.Meta = o
	} else {
		log.Error(err, "OnDelete missing Meta",
			"object", obj, "type", fmt.Sprintf("%T", obj))
		return
	}

	// Pull the runtime.Object out of the object
	if o, ok := obj.(runtime.Object); ok {
		d.Object = o
	} else {
		log.Error(nil, "OnDelete missing runtime.Object",
			"object", obj, "type", fmt.Sprintf("%T", obj))
		return
	}

	for _, p := range e.Predicates {
		if !p.Delete(d) {
			return
		}
	}

	// Invoke delete handler
	e.EventHandler.Delete(d, e.Queue)
}
