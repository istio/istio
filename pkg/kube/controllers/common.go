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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("controllers", "common controller logic", 0)

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
	gvk := u.GetObjectKind().GroupVersionKind()

	gk := config.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
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
func EnqueueForParentHandler(q Queue, kind config.GroupVersionKind) func(obj Object) {
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
					// Reference doesn't have namespace, but its always same-namespace, so use objects
					Namespace: obj.GetNamespace(),
					Name:      ref.Name,
				})
			}
		}
	}
	return handler
}

// ObjectHandler returns a handler that will act on the latest version of an object
// This means Add/Update/Delete are all handled the same and are just used to trigger reconciling.
func ObjectHandler(handler func(o Object)) cache.ResourceEventHandler {
	h := func(obj any) {
		o := extractObject(obj)
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
		o := extractObject(obj)
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
			oldObj := extractObject(oldInterface)
			if oldObj == nil {
				return
			}
			newObj := extractObject(newInterface)
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

func extractObject(obj any) Object {
	o, ok := obj.(Object)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Errorf("couldn't get object from tombstone %+v", obj)
			return nil
		}
		o, ok = tombstone.Obj.(Object)
		if !ok {
			log.Errorf("tombstone contained object that is not an object %+v", obj)
			return nil
		}
	}
	return o
}

// IgnoreNotFound returns nil on NotFound errors.
// All other values that are not NotFound errors or nil are returned unmodified.
func IgnoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
