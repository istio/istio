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

package controllerutil

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// AlreadyOwnedError is an error returned if the object you are trying to assign
// a controller reference is already owned by another controller Object is the
// subject and Owner is the reference for the current owner
type AlreadyOwnedError struct {
	Object v1.Object
	Owner  v1.OwnerReference
}

func (e *AlreadyOwnedError) Error() string {
	return fmt.Sprintf("Object %s/%s is already owned by another %s controller %s", e.Object.GetNamespace(), e.Object.GetName(), e.Owner.Kind, e.Owner.Name)
}

func newAlreadyOwnedError(Object v1.Object, Owner v1.OwnerReference) *AlreadyOwnedError {
	return &AlreadyOwnedError{
		Object: Object,
		Owner:  Owner,
	}
}

// SetControllerReference sets owner as a Controller OwnerReference on owned.
// This is used for garbage collection of the owned object and for
// reconciling the owner object on changes to owned (with a Watch + EnqueueRequestForOwner).
// Since only one OwnerReference can be a controller, it returns an error if
// there is another OwnerReference with Controller flag set.
func SetControllerReference(owner, object v1.Object, scheme *runtime.Scheme) error {
	ro, ok := owner.(runtime.Object)
	if !ok {
		return fmt.Errorf("is not a %T a runtime.Object, cannot call SetControllerReference", owner)
	}

	gvk, err := apiutil.GVKForObject(ro, scheme)
	if err != nil {
		return err
	}

	// Create a new ref
	ref := *v1.NewControllerRef(owner, schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind})

	existingRefs := object.GetOwnerReferences()
	fi := -1
	for i, r := range existingRefs {
		if referSameObject(ref, r) {
			fi = i
		} else if r.Controller != nil && *r.Controller {
			return newAlreadyOwnedError(object, r)
		}
	}
	if fi == -1 {
		existingRefs = append(existingRefs, ref)
	} else {
		existingRefs[fi] = ref
	}

	// Update owner references
	object.SetOwnerReferences(existingRefs)
	return nil
}

// Returns true if a and b point to the same object
func referSameObject(a, b v1.OwnerReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}

	return aGV == bGV && a.Kind == b.Kind && a.Name == b.Name
}

// OperationResult is the action result of a CreateOrUpdate call
type OperationResult string

const ( // They should complete the sentence "Deployment default/foo has been ..."
	// OperationResultNone means that the resource has not been changed
	OperationResultNone OperationResult = "unchanged"
	// OperationResultCreated means that a new resource is created
	OperationResultCreated OperationResult = "created"
	// OperationResultUpdated means that an existing resource is updated
	OperationResultUpdated OperationResult = "updated"
)

// CreateOrUpdate creates or updates the given object obj in the Kubernetes
// cluster. The object's desired state should be reconciled with the existing
// state using the passed in ReconcileFn. obj must be a struct pointer so that
// obj can be updated with the content returned by the Server.
//
// It returns the executed operation and an error.
func CreateOrUpdate(ctx context.Context, c client.Client, obj runtime.Object, f MutateFn) (OperationResult, error) {
	// op is the operation we are going to attempt
	op := OperationResultNone

	// get the existing object meta
	metaObj, ok := obj.(v1.Object)
	if !ok {
		return OperationResultNone, fmt.Errorf("%T does not implement metav1.Object interface", obj)
	}

	// retrieve the existing object
	key := client.ObjectKey{
		Name:      metaObj.GetName(),
		Namespace: metaObj.GetNamespace(),
	}
	err := c.Get(ctx, key, obj)

	// reconcile the existing object
	existing := obj.DeepCopyObject()
	existingObjMeta := existing.(v1.Object)
	existingObjMeta.SetName(metaObj.GetName())
	existingObjMeta.SetNamespace(metaObj.GetNamespace())

	if e := f(obj); e != nil {
		return OperationResultNone, e
	}

	if metaObj.GetName() != existingObjMeta.GetName() {
		return OperationResultNone, fmt.Errorf("ReconcileFn cannot mutate objects name")
	}

	if metaObj.GetNamespace() != existingObjMeta.GetNamespace() {
		return OperationResultNone, fmt.Errorf("ReconcileFn cannot mutate objects namespace")
	}

	if errors.IsNotFound(err) {
		err = c.Create(ctx, obj)
		op = OperationResultCreated
	} else if err == nil {
		if reflect.DeepEqual(existing, obj) {
			return OperationResultNone, nil
		}
		err = c.Update(ctx, obj)
		op = OperationResultUpdated
	} else {
		return OperationResultNone, err
	}

	if err != nil {
		op = OperationResultNone
	}
	return op, err
}

// MutateFn is a function which mutates the existing object into it's desired state.
type MutateFn func(existing runtime.Object) error
