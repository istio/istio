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

package config

import (
	"fmt"

	"github.com/golang/glog"

	brokerconfig "istio.io/api/broker/v1/config"
)

// Store describes a set of platform agnostic APIs that must be supported
// by the underlying platform to store and retrieve Istio configuration.
//
// Configuration key is defined to be a combination of the type, name, and
// namespace of the configuration object. The configuration key is guaranteed
// to be unique in the store.
//
// The storage interface presented here assumes that the underlying storage
// layer supports _Get_ (list), _Update_ (update), _Create_ (create) and
// _Delete_ semantics but does not guarantee any transactional semantics.
//
// _Update_, _Create_, and _Delete_ are mutator operations. These operations
// are asynchronous, and you might not see the effect immediately (e.g. _Get_
// might not return the object by key immediately after you mutate the store.)
// Intermittent errors might occur even though the operation succeeds, so you
// should always check if the object store has been modified even if the
// mutating operation returns an error.  Objects should be created with
// _Create_ operation and updated with _Update_ operation.
//
// Resource versions record the last mutation operation on each object. If a
// mutation is applied to a different revision of an object than what the
// underlying storage expects as defined by pure equality, the operation is
// blocked.  The client of this interface should not make assumptions about the
// structure or ordering of the revision identifier.
//
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them violates thread-safety.
type Store interface {
	// ConfigDescriptor exposes the configuration type schema known by the config store.
	// The type schema defines the bidrectional mapping between configuration
	// types and the protobuf encoding schema.
	Descriptor() Descriptor

	// Get retrieves a configuration element by a type and a key
	Get(typ, name, namespace string) (entry *Entry, exists bool)

	// List returns objects by type and namespace.
	// Use "" for the namespace to list across namespaces.
	List(typ, namespace string) ([]Entry, error)

	// Create adds a new configuration object to the store. If an object with the
	// same name and namespace for the type already exists, the operation fails
	// with no side effects.
	Create(entry Entry) (revision string, err error)

	// Update modifies an existing configuration object in the store.  Update
	// requires that the object has been created.  Resource version prevents
	// overriding a value that has been changed between prior _Get_ and _Put_
	// operation to achieve optimistic concurrency. This method returns a new
	// revision if the operation succeeds.
	Update(entry Entry) (newRevision string, err error)

	// Delete removes an object from the store by key
	Delete(typ, name, namespace string) error
}

// Key function for the configuration objects
func Key(typ, name, namespace string) string {
	return fmt.Sprintf("%s/%s/%s", typ, namespace, name)
}

// Key is the unique identifier for a configuration object
func (entry *Entry) Key() string {
	return Key(entry.Type, entry.Name, entry.Namespace)
}

// BrokerConfigStore is a specialized interface to access config store using
// Broker configuration types.
type BrokerConfigStore interface {
	// ServiceClasses lists all service classes.
	ServiceClasses() map[string]*brokerconfig.ServiceClass

	// ServicePlans lists all service plans.
	ServicePlans() map[string]*brokerconfig.ServicePlan

	// ServicePlansByService lists all service plans contains the specified service class
	ServicePlansByService(service string) map[string]*brokerconfig.ServicePlan
}

const (
	// IstioAPIGroup defines API group name for Istio configuration resources
	IstioAPIGroup = "config.istio.io"

	// IstioAPIVersion defines API group version
	IstioAPIVersion = "v1alpha2"
)

var (
	// ServiceClass describes service class
	ServiceClass = Schema{
		Type:        "service-class",
		Plural:      "service-classes",
		MessageName: "istio.broker.v1.config.ServiceClass",
	}

	// ServicePlan describes service plan
	ServicePlan = Schema{
		Type:        "service-plan",
		Plural:      "service-plans",
		MessageName: "istio.broker.v1.config.ServicePlan",
	}

	// BrokerConfigTypes lists all types with schemas and validation
	BrokerConfigTypes = Descriptor{
		ServiceClass,
		ServicePlan,
	}
)

// brokerConfigStore provides a simple adapter for Broker configuration types
// from the generic config registry
type brokerConfigStore struct {
	Store
}

// MakeBrokerConfigStore creates a wrapper around a store
func MakeBrokerConfigStore(store Store) BrokerConfigStore {
	return &brokerConfigStore{store}
}

func (i brokerConfigStore) ServiceClasses() map[string]*brokerconfig.ServiceClass {
	out := make(map[string]*brokerconfig.ServiceClass)
	rs, err := i.List(ServiceClass.Type, "")
	if err != nil {
		glog.V(2).Infof("ServiceClasses => %v", err)
		return out
	}
	for _, r := range rs {
		if c, ok := r.Spec.(*brokerconfig.ServiceClass); ok {
			out[r.Key()] = c
		}
	}
	return out
}

func (i brokerConfigStore) ServicePlans() map[string]*brokerconfig.ServicePlan {
	out := make(map[string]*brokerconfig.ServicePlan)
	rs, err := i.List(ServicePlan.Type, "")
	if err != nil {
		glog.V(2).Infof("ServicePlans => %v", err)
		return out
	}
	for _, r := range rs {
		if c, ok := r.Spec.(*brokerconfig.ServicePlan); ok {
			out[r.Key()] = c
		}
	}
	return out
}

func (i brokerConfigStore) ServicePlansByService(service string) map[string]*brokerconfig.ServicePlan {
	out := make(map[string]*brokerconfig.ServicePlan)
	rs := i.ServicePlans()
	for k, v := range rs {
		for _, s := range v.GetServices() {
			if s == service {
				out[k] = v
				break
			}
		}
	}

	return out
}
