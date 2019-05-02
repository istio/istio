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

package handler

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var _ EventHandler = &EnqueueRequestForOwner{}

var log = logf.KBLog.WithName("eventhandler").WithName("EnqueueRequestForOwner")

// EnqueueRequestForOwner enqueues Requests for the Owners of an object.  E.g. the object that created
// the object that was the source of the Event.
//
// If a ReplicaSet creates Pods, users may reconcile the ReplicaSet in response to Pod Events using:
//
// - a source.Kind Source with Type of Pod.
//
// - a handler.EnqueueRequestForOwner EventHandler with an OwnerType of ReplicaSet and IsController set to true.
type EnqueueRequestForOwner struct {
	// OwnerType is the type of the Owner object to look for in OwnerReferences.  Only Group and Kind are compared.
	OwnerType runtime.Object

	// IsController if set will only look at the first OwnerReference with Controller: true.
	IsController bool

	// groupKind is the cached Group and Kind from OwnerType
	groupKind schema.GroupKind
}

// Create implements EventHandler
func (e *EnqueueRequestForOwner) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	for _, req := range e.getOwnerReconcileRequest(evt.Meta) {
		q.Add(req)
	}
}

// Update implements EventHandler
func (e *EnqueueRequestForOwner) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	for _, req := range e.getOwnerReconcileRequest(evt.MetaOld) {
		q.Add(req)
	}
	for _, req := range e.getOwnerReconcileRequest(evt.MetaNew) {
		q.Add(req)
	}
}

// Delete implements EventHandler
func (e *EnqueueRequestForOwner) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	for _, req := range e.getOwnerReconcileRequest(evt.Meta) {
		q.Add(req)
	}
}

// Generic implements EventHandler
func (e *EnqueueRequestForOwner) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	for _, req := range e.getOwnerReconcileRequest(evt.Meta) {
		q.Add(req)
	}
}

// parseOwnerTypeGroupKind parses the OwnerType into a Group and Kind and caches the result.  Returns false
// if the OwnerType could not be parsed using the scheme.
func (e *EnqueueRequestForOwner) parseOwnerTypeGroupKind(scheme *runtime.Scheme) error {
	// Get the kinds of the type
	kinds, _, err := scheme.ObjectKinds(e.OwnerType)
	if err != nil {
		log.Error(err, "Could not get ObjectKinds for OwnerType", "owner type", fmt.Sprintf("%T", e.OwnerType))
		return err
	}
	// Expect only 1 kind.  If there is more than one kind this is probably an edge case such as ListOptions.
	if len(kinds) != 1 {
		err := fmt.Errorf("Expected exactly 1 kind for OwnerType %T, but found %s kinds", e.OwnerType, kinds)
		log.Error(nil, "Expected exactly 1 kind for OwnerType", "owner type", fmt.Sprintf("%T", e.OwnerType), "kinds", kinds)
		return err

	}
	// Cache the Group and Kind for the OwnerType
	e.groupKind = schema.GroupKind{Group: kinds[0].Group, Kind: kinds[0].Kind}
	return nil
}

// getOwnerReconcileRequest looks at object and returns a slice of reconcile.Request to reconcile
// owners of object that match e.OwnerType.
func (e *EnqueueRequestForOwner) getOwnerReconcileRequest(object metav1.Object) []reconcile.Request {
	// Iterate through the OwnerReferences looking for a match on Group and Kind against what was requested
	// by the user
	var result []reconcile.Request
	for _, ref := range e.getOwnersReferences(object) {
		// Parse the Group out of the OwnerReference to compare it to what was parsed out of the requested OwnerType
		refGV, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			log.Error(err, "Could not parse OwnerReference APIVersion",
				"api version", ref.APIVersion)
			return nil
		}

		// Compare the OwnerReference Group and Kind against the OwnerType Group and Kind specified by the user.
		// If the two match, create a Request for the objected referred to by
		// the OwnerReference.  Use the Name from the OwnerReference and the Namespace from the
		// object in the event.
		if ref.Kind == e.groupKind.Kind && refGV.Group == e.groupKind.Group {
			// Match found - add a Request for the object referred to in the OwnerReference
			result = append(result, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: object.GetNamespace(),
				Name:      ref.Name,
			}})
		}
	}

	// Return the matches
	return result
}

// getOwnersReferences returns the OwnerReferences for an object as specified by the EnqueueRequestForOwner
// - if IsController is true: only take the Controller OwnerReference (if found)
// - if IsController is false: take all OwnerReferences
func (e *EnqueueRequestForOwner) getOwnersReferences(object metav1.Object) []metav1.OwnerReference {
	if object == nil {
		return nil
	}

	// If not filtered as Controller only, then use all the OwnerReferences
	if !e.IsController {
		return object.GetOwnerReferences()
	}
	// If filtered to a Controller, only take the Controller OwnerReference
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		return []metav1.OwnerReference{*ownerRef}
	}
	// No Controller OwnerReference found
	return nil
}

var _ inject.Scheme = &EnqueueRequestForOwner{}

// InjectScheme is called by the Controller to provide a singleton scheme to the EnqueueRequestForOwner.
func (e *EnqueueRequestForOwner) InjectScheme(s *runtime.Scheme) error {
	return e.parseOwnerTypeGroupKind(s)
}
