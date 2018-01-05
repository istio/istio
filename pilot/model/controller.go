// Copyright 2017 Istio Authors
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

package model

// Controller provides a platform independent means to rationalize
// naming and discovery of mesh entities within the Pilot. The Controller is
// intended to be an event control loop for a variety of mesh entities like
// services and service instances. The  Proxy agent registers itself
// with the controller loop and receives periodic notifications on the
// view of mesh resources as seen from the native platform's point of view
// and allows the Pilot to maintain an aggregate view across various
// platforms and environments.
//
// The controller guarantees the following consistency requirement:
// Notifications from each controller must be serialized such that a notification
// representing the controller's native view it constructed at T0 preceds the
// notification of one constructed by that controller at T1.
type Controller interface {

	// Invoked by the aggregated MeshView for setting up
	// a handler for listening on notifications to
	// reconcile the controller's view with the aggregated view.
	// This method must be called before Run(). Path is the
	// a unique identity for this controller from the aggregated
	// mesh point of view. The root of the path is the platform
	// ex: /kube/. Upcoming PRs will introduce clusters ex:
	// /kube/cluster-a/
	Handle(path string, handler *ControllerViewHandler) error

	// Run until a signal is received. Constructing the
	// native view of a platforms resources must commence
	// after Run has been called.
	// The supplied channel stop, is expected to be reused across
	// multiple sub-components of the Controller. The caller must
	// call close(stop) to ensure that this Controller is
	// properly shutdown.
	Run(stop <-chan struct{})
}

// ControllerView is the native view as seen by the controller for
// mesh entities involved with naming and discovery. ControllerView
// must be complete for the path specified by the view.
// i.e. Entities of all types must be fully populated if
// they have native representation.
type ControllerView struct {

	// The path must always start with the root passed via
	// handle(). However controllers may choose to shard
	// updates as long as entities under each shard are
	// distinct. For ex: entities under /kube/cluster-a/abc
	// would be considered exclusive from /kube/cluster-a/def.
	// The controller must keep track of shard lifecycles
	// meaning, if a shard is no longer needed, there must
	// be a call with an empty list of resources to clear
	// the aggregated view. The aggregated view does not
	// do anything special with shards.
	Path string

	// Service entities specific to this controller and path
	// Note: services can have the same name across
	Services         []*Service
	ServiceInstances []*ServiceInstance
}

// ControllerViewHandler is an interface for synchronizing the
// aggregated mesh view with the native view of resources under
// a platform controller's influence.
type ControllerViewHandler interface {

	// Reconciles the controller's view of entities.
	// There should be no expectations on ordering of how
	// various mesh entities get reconciled with the
	// aggregated view. This is true between entity types
	// and within each type. For example, Services may get
	// reconciled after instances. Instances may get reconciled
	// in batches. The only guarantee is that by the time to
	// Handle() returns, all entities are fully reconciled
	// with the aggregated mesh view.
	Reconcile(*ControllerView)
}

// CacheEvictionHandler allows implementors to properly clear
// caches of model.* objects.
type CacheEvictionHandler interface {
	// Called by the model when the underlying model.* objects
	// of the kind specified in CacheReferences has changed.
	EvictCache(CacheReferences)
}

// CacheReferences are references maintained by the model for the
// explicit purpose of maintaining caches of model.* objects.
// Cache maintainers must receive CacheReferences along with
// query results so that they can map model keys to the key
// representing the query. Cache maintainers must also implement
// CacheEvictionHandler. This allows cache maintainers to invalidate
// caches for relevant query keys.
type CacheReferences struct {
	// The kind of model.* object that is intended for caching
	// Examples could be: config.istio.io/Service or
	// config.istio.io/ServiceInstance
	Kind string

	// The keyset of model.* object keys. Cache handlers should
	// expect the key format to be an implementation detail of
	// the model. However the key value must be unique for
	// each of the model.* object it respresents. Model implementors
	// must guarantee uniqueness across the mesh. The keys in this
	// keyset are distinct from cache keys but may be asssociated
	// with cache keys. Events from CacheEvictionHandler would then
	// use these keys to find the associated cache keys and remove
	// them from relevant caches.
	Keyset map[string]bool
}

// UpdateNotifier is an interface intended for implementors of
// queries that return model.* objects for the purpose of managing
// cache evictions.
type UpdateNotifier interface {
	// Sets the cache eviction handler for notifying
	// the caller on changes to model objects.
	SetCacheEvictionHandler(CacheEvictionHandler) error
}

// Event represents a registry update event, mostly for
// cache handling.
type Event int

const (
	// EventAdd is sent when an object is added
	EventAdd Event = iota

	// EventUpdate is sent when an object is modified
	// Captures the modified object
	EventUpdate

	// EventDelete is sent when an object is deleted
	// Captures the object at the last known state
	EventDelete
)

// NewCacheReferences creates CacheReferences for a specific
// cache kind
func NewCacheReferences(Kind string) *CacheReferences {
	return &CacheReferences{
		Kind:   Kind,
		Keyset: map[string]bool{},
	}
}

// Equals compares one set of cache references with another
func (cr *CacheReferences) Equals(other *CacheReferences) bool {
	if cr.Kind != other.Kind {
		return false
	}
	if len(cr.Keyset) != len(other.Keyset) {
		return false
	}
	for key, val := range cr.Keyset {
		otherVal, exists := other.Keyset[key]
		if !exists {
			return false
		}
		if val != otherVal {
			return false
		}
	}
	return true
}

// Utility function mostly intended for logging cache refrerences
func (cr *CacheReferences) String() string {
	out := "Kind: " + cr.Kind + ", Keyset: ["
	for key, exists := range cr.Keyset {
		if exists {
			out = out + key + ", "
		}
	}
	return out + "]"
}

func (event Event) String() string {
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
