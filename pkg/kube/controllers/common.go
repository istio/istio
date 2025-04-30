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
	"cmp"
	"fmt"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

var log = istiolog.RegisterScope("controllers", "common controller logic")

// Object is a union of runtime + meta objects. Essentially every k8s object meets this interface.
// and certainly all that we care about.
type Object interface {
	metav1.Object
	runtime.Object
}

type ComparableObject interface {
	comparable
	Object
}

// IsNil works around comparing generic types
func IsNil[O comparable](o O) bool {
	var t O
	return o == t
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
	found, ok := gvk.ToGVR(gk)
	if !ok {
		return res, fmt.Errorf("unknown gvk: %v", gk)
	}
	return found, nil
}

// ObjectToGVR extracts the GVR of an unstructured resource. This is useful when using dynamic
// clients.
func ObjectToGVR(u Object) (schema.GroupVersionResource, error) {
	g := u.GetObjectKind().GroupVersionKind()

	gk := config.GroupVersionKind{
		Group:   g.Group,
		Version: g.Version,
		Kind:    g.Kind,
	}
	found, ok := gvk.ToGVR(gk)
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown gvk: %v", gk)
	}
	return found, nil
}

// EnqueueForParentHandler returns a handler that will enqueue the parent (by ownerRef) resource
func EnqueueForParentHandler(q Queue, kind config.GroupVersionKind) func(obj Object) {
	handler := func(obj Object) {
		for _, ref := range obj.GetOwnerReferences() {
			refGV, err := schema.ParseGroupVersion(ref.APIVersion)
			if err != nil {
				log.Errorf("could not parse OwnerReference api version %q: %v", ref.APIVersion, err)
				continue
			}
			if refGV.Group == kind.Group && ref.Kind == kind.Kind {
				// We found a parent we care about, add it to the queue
				q.Add(types.NamespacedName{
					// Reference doesn't have namespace, but its always same-namespace, so use objects
					Namespace: obj.GetNamespace(),
					Name:      ref.Name,
				})
			}
		}
	}
	return handler
}

// EventType represents a registry update event
type EventType int

const (
	// EventAdd is sent when an object is added
	EventAdd EventType = iota

	// EventUpdate is sent when an object is modified
	// Captures the modified object
	EventUpdate

	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state
	EventDelete
)

func (event EventType) String() string {
	out := "unknown"
	switch event {
	case EventAdd:
		out = "add"
	case EventUpdate:
		out = "update"
	case EventDelete:
		out = "delete"
	}
	return out
}

type Event struct {
	Old   Object
	New   Object
	Event EventType
}

func (e Event) Latest() Object {
	if e.New != nil {
		return e.New
	}
	return e.Old
}

func FromEventHandler(handler func(o Event)) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			o := ExtractObject(obj)
			if o == nil {
				return
			}
			handler(Event{
				New:   o,
				Event: EventAdd,
			})
		},
		UpdateFunc: func(oldInterface, newInterface any) {
			oldObj := ExtractObject(oldInterface)
			if oldObj == nil {
				return
			}
			newObj := ExtractObject(newInterface)
			if newObj == nil {
				return
			}
			handler(Event{
				Old:   oldObj,
				New:   newObj,
				Event: EventUpdate,
			})
		},
		DeleteFunc: func(obj any) {
			o := ExtractObject(obj)
			if o == nil {
				return
			}
			handler(Event{
				Old:   o,
				Event: EventDelete,
			})
		},
	}
}

// ObjectHandler returns a handler that will act on the latest version of an object
// This means Add/Update/Delete are all handled the same and are just used to trigger reconciling.
func ObjectHandler(handler func(o Object)) cache.ResourceEventHandler {
	h := func(obj any) {
		o := ExtractObject(obj)
		if o == nil {
			return
		}
		handler(o)
	}
	return cache.ResourceEventHandlerFuncs{
		AddFunc: h,
		UpdateFunc: func(oldObj, newObj any) {
			h(newObj)
		},
		DeleteFunc: h,
	}
}

// FilteredObjectHandler returns a handler that will act on the latest version of an object
// This means Add/Update/Delete are all handled the same and are just used to trigger reconciling.
// If filters are set, returning 'false' will exclude the event. For Add and Deletes, the filter will be based
// on the new or old item. For updates, the item will be handled if either the new or the old object is updated.
func FilteredObjectHandler(handler func(o Object), filter func(o Object) bool) cache.ResourceEventHandler {
	return filteredObjectHandler(handler, false, filter)
}

// FilteredObjectSpecHandler returns a handler that will act on the latest version of an object
// This means Add/Update/Delete are all handled the same and are just used to trigger reconciling.
// Unlike FilteredObjectHandler, the handler is only trigger when the resource spec changes (ie resourceVersion)
// If filters are set, returning 'false' will exclude the event. For Add and Deletes, the filter will be based
// on the new or old item. For updates, the item will be handled if either the new or the old object is updated.
func FilteredObjectSpecHandler(handler func(o Object), filter func(o Object) bool) cache.ResourceEventHandler {
	return filteredObjectHandler(handler, true, filter)
}

func filteredObjectHandler(handler func(o Object), onlyIncludeSpecChanges bool, filter func(o Object) bool) cache.ResourceEventHandler {
	single := func(obj any) {
		o := ExtractObject(obj)
		if o == nil {
			return
		}
		if !filter(o) {
			return
		}
		handler(o)
	}
	return cache.ResourceEventHandlerFuncs{
		AddFunc: single,
		UpdateFunc: func(oldInterface, newInterface any) {
			oldObj := ExtractObject(oldInterface)
			if oldObj == nil {
				return
			}
			newObj := ExtractObject(newInterface)
			if newObj == nil {
				return
			}
			if onlyIncludeSpecChanges && oldObj.GetResourceVersion() == newObj.GetResourceVersion() {
				return
			}
			newer := filter(newObj)
			older := filter(oldObj)
			if !newer && !older {
				return
			}
			handler(newObj)
		},
		DeleteFunc: single,
	}
}

// Extract pulls a T from obj, handling tombstones.
// This will return nil if the object cannot be extracted.
func Extract[T Object](obj any) T {
	var empty T
	if obj == nil {
		return empty
	}
	o, ok := obj.(T)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("couldn't get object from tombstone: (%T vs %T) %+v", obj, ptr.Empty[T](), obj)
			return empty
		}
		o, ok = tombstone.Obj.(T)
		if !ok {
			log.Errorf("tombstone contained object that is not an object (key:%v, obj:%T)", tombstone.Key, tombstone.Obj)
			return empty
		}
	}
	return o
}

func ExtractObject(obj any) Object {
	return Extract[Object](obj)
}

// IgnoreNotFound returns nil on NotFound errors.
// All other values that are not NotFound errors or nil are returned unmodified.
func IgnoreNotFound(err error) error {
	if kerrors.IsNotFound(err) {
		return nil
	}
	return err
}

// EventHandler mirrors ResourceEventHandlerFuncs, but takes typed T objects instead of any.
type EventHandler[T Object] struct {
	AddFunc         func(obj T)
	AddExtendedFunc func(obj T, initialSync bool)
	UpdateFunc      func(oldObj, newObj T)
	DeleteFunc      func(obj T)
}

func (e EventHandler[T]) OnAdd(obj interface{}, initialSync bool) {
	if e.AddExtendedFunc != nil {
		e.AddExtendedFunc(Extract[T](obj), initialSync)
	} else if e.AddFunc != nil {
		e.AddFunc(Extract[T](obj))
	}
}

func (e EventHandler[T]) OnUpdate(oldObj, newObj interface{}) {
	if e.UpdateFunc != nil {
		e.UpdateFunc(Extract[T](oldObj), Extract[T](newObj))
	}
}

func (e EventHandler[T]) OnDelete(obj interface{}) {
	if e.DeleteFunc != nil {
		e.DeleteFunc(Extract[T](obj))
	}
}

var _ cache.ResourceEventHandler = EventHandler[Object]{}

type Shutdowner interface {
	ShutdownHandlers()
}

// ShutdownAll is a simple helper to shutdown all informers
func ShutdownAll(s ...Shutdowner) {
	for _, h := range s {
		h.ShutdownHandlers()
	}
}

func OldestObject[T Object](configs []T) T {
	return slices.MinFunc(configs, func(i, j T) int {
		if r := i.GetCreationTimestamp().Compare(j.GetCreationTimestamp().Time); r != 0 {
			return r
		}
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(i.GetName(), j.GetName()); r != 0 {
			return r
		}
		return cmp.Compare(i.GetNamespace(), j.GetNamespace())
	})
}
