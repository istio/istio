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

/*
Package pkg provides libraries for building Controllers.  Controllers implement Kubernetes APIs
and are foundational to building Operators, Workload APIs, Configuration APIs, Autoscalers, and more.

Client

Client provides a Read + Write client for reading and writing Kubernetes objects.

Cache

Cache provides a Read client for reading objects from a local cache.
A cache may register handlers to respond to events that update the cache.

Manager

Manager is required for creating a Controller and provides the Controller shared dependencies such as
clients, caches, schemes, etc.  Controllers should be Started through the Manager by calling Manager.Start.

Controller

Controller implements a Kubernetes API by responding to events (object Create, Update, Delete) and ensuring that
the state specified in the Spec of the object matches the state of the system.  This is called a Reconciler.
If they do not match, the Controller will create / update / delete objects as needed to make them match.

Controllers are implemented as worker queues that process reconcile.Requests (requests to Reconciler the
state for a specific object).

Unlike http handlers, Controllers DO NOT handle events directly, but enqueue Requests to eventually Reconciler
the object.  This means the handling of multiple events may be batched together and the full state of the
system must be read for each Reconciler.

* Controllers require a Reconciler to be provided to perform the work pulled from the work queue.

* Controllers require Watches to be configured to enqueue reconcile.Requests in response to events.

Webhook

Admission Webhooks are a mechanism for extending kubernetes APIs. Webhooks can be configured with target
event type (object Create, Update, Delete), the API server will send AdmissionRequests to them
when certain events happen. The webhooks may mutate and (or) validate the object embedded in
the AdmissionReview requests and send back the response to the API server.

There are 2 types of admission webhook: mutating and validating admission webhook.
Mutating webhook is used to mutate a core API object or a CRD instance before the API server admits it.
Validating webhook is used to validate if an object meets certain requirements.

* Admission Webhooks require Handler(s) to be provided to process the received AdmissionReview requests.

Reconciler

Reconciler is a function provided to a Controller that may be called at anytime with the Name and Namespace of an object.
When called, Reconciler will ensure that the state of the system matches what is specified in the object at the
time Reconciler is called.

Example: Reconciler invoked for a ReplicaSet object.  The ReplicaSet specifies 5 replicas but only
3 Pods exist in the system.  Reconciler creates 2 more Pods and sets their OwnerReference to point at the
ReplicaSet with controller=true.

* Reconciler contains all of the business logic of a Controller.

* Reconciler typically works on a single object type. - e.g. it will only reconcile ReplicaSets.  For separate
types use separate Controllers. If you wish to trigger reconciles from other objects, you can provide
a mapping (e.g. owner references) that maps the object that triggers the reconcile to the object being reconciled.

* Reconciler is provided the Name / Namespace of the object to reconcile.

* Reconciler does not care about the event contents or event type responsible for triggering the Reconciler.
- e.g. it doesn't matter whether a ReplicaSet was created or updated, Reconciler will always compare the number of
Pods in the system against what is specified in the object at the time it is called.

Source

resource.Source is an argument to Controller.Watch that provides a stream of events.
Events typically come from watching Kubernetes APIs (e.g. Pod Create, Update, Delete).

Example: source.Kind uses the Kubernetes API Watch endpoint for a GroupVersionKind to provide
Create, Update, Delete events.

* Source provides a stream of events (e.g. object Create, Update, Delete) for Kubernetes objects typically
through the Watch API.

* Users SHOULD only use the provided Source implementations instead of implementing their own for nearly all cases.

EventHandler

handler.EventHandler is a argument to Controller.Watch that enqueues reconcile.Requests in response to events.

Example: a Pod Create event from a Source is provided to the eventhandler.EnqueueHandler, which enqueues a
reconcile.Request containing the name / Namespace of the Pod.

* EventHandlers handle events by enqueueing reconcile.Requests for one or more objects.

* EventHandlers MAY map an event for an object to a reconcile.Request for an object of the same type.

* EventHandlers MAY map an event for an object to a reconcile.Request for an object of a different type - e.g.
map a Pod event to a reconcile.Request for the owning ReplicaSet.

* EventHandlers MAY map an event for an object to multiple reconcile.Requests for objects of the same or a different
type - e.g. map a Node event to objects that respond to cluster resize events.

* Users SHOULD only use the provided EventHandler implementations instead of implementing their own for almost
all cases.

Predicate

predicate.Predicate is an optional argument to Controller.Watch that filters events.  This allows common filters to be
reused and composed.

* Predicate takes an event and returns a bool (true to enqueue)

* Predicates are optional arguments

* Users SHOULD use the provided Predicate implementations, but MAY implement additional
Predicates e.g. generation changed, label selectors changed etc.

PodController Diagram

Source provides event:

* &source.KindSource{&v1.Pod{}} -> (Pod foo/bar Create Event)

EventHandler enqueues Request:

* &handler.EnqueueRequestForObject{} -> (reconcile.Request{types.NamespaceName{Name: "foo", Namespace: "bar"}})

Reconciler is called with the Request:

* Reconciler(reconcile.Request{types.NamespaceName{Name: "foo", Namespace: "bar"}})

Usage

The following example shows creating a new Controller program which Reconciles ReplicaSet objects in response
to Pod or ReplicaSet events.  The Reconciler function simply adds a label to the ReplicaSet.

See the example/main.go for a usage example.

Controller Example

1. Watch ReplicaSet and Pods Sources

1.1 ReplicaSet -> handler.EnqueueRequestForObject - enqueue a Request with the ReplicaSet Namespace and Name.

1.2 Pod (created by ReplicaSet) -> handler.EnqueueRequestForOwnerHandler - enqueue a Request with the
Owning ReplicaSet Namespace and Name.

2. Reconciler ReplicaSet in response to an event

2.1 ReplicaSet object created -> Read ReplicaSet, try to read Pods -> if is missing create Pods.

2.2 Reconciler triggered by creation of Pods -> Read ReplicaSet and Pods, do nothing.

2.3 Reconciler triggered by deletion of Pods from some other actor -> Read ReplicaSet and Pods, create replacement Pods.

Watching and EventHandling

Controllers may Watch multiple Kinds of objects (e.g. Pods, ReplicaSets and Deployments), but they Reconciler
only a single Type.  When one Type of object must be updated in response to changes in another Type of object,
an EnqueueRequestFromMapFunc may be used to map events from one type to another.  e.g. Respond to a cluster resize
event (add / delete Node) by re-reconciling all instances of some API.

A Deployment Controller might use an EnqueueRequestForObject and EnqueueRequestForOwner to:

* Watch for Deployment Events - enqueue the Namespace and Name of the Deployment.

* Watch for ReplicaSet Events - enqueue the Namespace and Name of the Deployment that created the ReplicaSet
(e.g the Owner)

Note: reconcile.Requests are deduplicated when they are enqueued.  Many Pod Events for the same ReplicaSet
may trigger only 1 reconcile invocation as each Event results in the Handler trying to enqueue
the same reconcile.Request for the ReplicaSet.

Controller Writing Tips

Reconciler Runtime Complexity:

* It is better to write Controllers to perform an O(1) Reconciler N times (e.g. on N different objects) instead of
performing an O(N) Reconciler 1 time (e.g. on a single object which manages N other objects).

* Example: If you need to update all Services in response to a Node being added - Reconciler Services but Watch
Nodes (transformed to Service object name / Namespaces) instead of Reconciling Nodes and updating Services

Event Multiplexing:

* reconcile.Requests for the same Name / Namespace are batched and deduplicated when they are enqueued.  This allows
Controllers to gracefully handle a high volume of events for a single object.  Multiplexing multiple event Sources to
a single object Type will batch requests across events for different object types.

* Example: Pod events for a ReplicaSet are transformed to a ReplicaSet Name / Namespace, so the ReplicaSet
will be Reconciled only 1 time for multiple events from multiple Pods.
*/
package pkg
