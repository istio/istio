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

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model/test"
)

// ConfigMeta is metadata attached to each configuration unit.
// The revision is optional, and if provided, identifies the
// last update operation on the object.
type ConfigMeta struct {
	// Type is a short configuration name that matches the content message type
	// (e.g. "route-rule")
	Type string `json:"type,omitempty"`

	// Name is a unique immutable identifier in a namespace
	Name string `json:"name,omitempty"`

	// Namespace defines the space for names (optional for some types),
	// applications may choose to use namespaces for a variety of purposes
	// (security domains, fault domains, organizational domains)
	Namespace string `json:"namespace,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`

	// ResourceVersion is an opaque identifier for tracking updates to the config registry.
	// The implementation may use a change index or a commit log for the revision.
	// The config client should not make any assumptions about revisions and rely only on
	// exact equality to implement optimistic concurrency of read-write operations.
	//
	// The lifetime of an object of a particular revision depends on the underlying data store.
	// The data store may compactify old revisions in the interest of storage optimization.
	//
	// An empty revision carries a special meaning that the associated object has
	// not been stored and assigned a revision.
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.
type Config struct {
	ConfigMeta

	// Spec holds the configuration object as a protobuf message
	Spec proto.Message
}

// ConfigStore describes a set of platform agnostic APIs that must be supported
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
type ConfigStore interface {
	// ConfigDescriptor exposes the configuration type schema known by the config store.
	// The type schema defines the bidrectional mapping between configuration
	// types and the protobuf encoding schema.
	ConfigDescriptor() ConfigDescriptor

	// Get retrieves a configuration element by a type and a key
	Get(typ, name, namespace string) (config *Config, exists bool)

	// List returns objects by type and namespace.
	// Use "" for the namespace to list across namespaces.
	List(typ, namespace string) ([]Config, error)

	// Create adds a new configuration object to the store. If an object with the
	// same name and namespace for the type already exists, the operation fails
	// with no side effects.
	Create(config Config) (revision string, err error)

	// Update modifies an existing configuration object in the store.  Update
	// requires that the object has been created.  Resource version prevents
	// overriding a value that has been changed between prior _Get_ and _Put_
	// operation to achieve optimistic concurrency. This method returns a new
	// revision if the operation succeeds.
	Update(config Config) (newRevision string, err error)

	// Delete removes an object from the store by key
	Delete(typ, name, namespace string) error
}

// Key function for the configuration objects
func Key(typ, name, namespace string) string {
	return fmt.Sprintf("%s/%s/%s", typ, namespace, name)
}

// Key is the unique identifier for a configuration object
func (config *Config) Key() string {
	return Key(config.Type, config.Name, config.Namespace)
}

// ConfigStoreCache is a local fully-replicated cache of the config store.  The
// cache actively synchronizes its local state with the remote store and
// provides a notification mechanism to receive update events. As such, the
// notification handlers must be registered prior to calling _Run_, and the
// cache requires initial synchronization grace period after calling  _Run_.
//
// Update notifications require the following consistency guarantee: the view
// in the cache must be AT LEAST as fresh as the moment notification arrives, but
// MAY BE more fresh (e.g. if _Delete_ cancels an _Add_ event).
//
// Handlers execute on the single worker queue in the order they are appended.
// Handlers receive the notification event and the associated object.  Note
// that all handlers must be registered before starting the cache controller.
type ConfigStoreCache interface {
	ConfigStore

	// RegisterEventHandler adds a handler to receive config update events for a
	// configuration type
	RegisterEventHandler(typ string, handler func(Config, Event))

	// Run until a signal is received
	Run(stop <-chan struct{})

	// HasSynced returns true after initial cache synchronization is complete
	HasSynced() bool
}

// ConfigDescriptor defines the bijection between the short type name and its
// fully qualified protobuf message name
type ConfigDescriptor []ProtoSchema

// ProtoSchema provides description of the configuration schema and its key function
type ProtoSchema struct {
	// Type refers to the short configuration type name
	Type string

	// Plural refers to the short plural configuration name
	Plural string

	// MessageName refers to the protobuf message type name corresponding to the type
	MessageName string

	// Validate configuration as a protobuf message assuming the object is an
	// instance of the expected message type
	Validate func(config proto.Message) error
}

// Types lists all known types in the config schema
func (descriptor ConfigDescriptor) Types() []string {
	types := make([]string, 0, len(descriptor))
	for _, t := range descriptor {
		types = append(types, t.Type)
	}
	return types
}

// GetByMessageName finds a schema by message name if it is available
func (descriptor ConfigDescriptor) GetByMessageName(name string) (ProtoSchema, bool) {
	for _, schema := range descriptor {
		if schema.MessageName == name {
			return schema, true
		}
	}
	return ProtoSchema{}, false
}

// GetByType finds a schema by type if it is available
func (descriptor ConfigDescriptor) GetByType(name string) (ProtoSchema, bool) {
	for _, schema := range descriptor {
		if schema.Type == name {
			return schema, true
		}
	}
	return ProtoSchema{}, false
}

// IstioConfigStore is a specialized interface to access config store using
// Istio configuration types
type IstioConfigStore interface {
	// RouteRules lists all routing rules
	RouteRules() map[string]*proxyconfig.RouteRule

	// IngressRules lists all ingress rules
	IngressRules() map[string]*proxyconfig.IngressRule

	// DestinationPolicies lists all destination rules
	DestinationPolicies() []*proxyconfig.DestinationPolicy

	// RouteRulesBySource selects routing rules by source service instances.
	// A rule must match at least one of the input service instances since the proxy
	// does not distinguish between source instances in the request.
	// The rules are sorted by precedence (high first) in a stable manner.
	RouteRulesBySource(instances []*ServiceInstance) []*proxyconfig.RouteRule

	// DestinationPolicy returns a policy for a service version.
	DestinationPolicy(destination string, tags Tags) *proxyconfig.DestinationVersionPolicy
}

const (
	// IstioAPIGroup defines API group name for Istio configuration resources
	IstioAPIGroup = "config.istio.io"

	// IstioAPIVersion defines API group version
	IstioAPIVersion = "v1alpha2"

	// HeaderURI is URI HTTP header
	HeaderURI = "uri"

	// HeaderAuthority is authority HTTP header
	HeaderAuthority = "authority"
)

var (
	// MockConfig is used purely for testing
	MockConfig = ProtoSchema{
		Type:        "mock-config",
		Plural:      "mock-configs",
		MessageName: "test.MockConfig",
		Validate: func(config proto.Message) error {
			if config.(*test.MockConfig).Key == "" {
				return errors.New("empty key")
			}
			return nil
		},
	}

	// RouteRule describes route rules
	RouteRule = ProtoSchema{
		Type:        "route-rule",
		Plural:      "route-rules",
		MessageName: "istio.proxy.v1.config.RouteRule",
		Validate:    ValidateRouteRule,
	}

	// IngressRule describes ingress rules
	IngressRule = ProtoSchema{
		Type:        "ingress-rule",
		Plural:      "ingress-rules",
		MessageName: "istio.proxy.v1.config.IngressRule",
		Validate:    ValidateIngressRule,
	}

	// DestinationPolicy describes destination rules
	DestinationPolicy = ProtoSchema{
		Type:        "destination-policy",
		Plural:      "destination-policies",
		MessageName: "istio.proxy.v1.config.DestinationPolicy",
		Validate:    ValidateDestinationPolicy,
	}

	// IstioConfigTypes lists all Istio config types with schemas and validation
	IstioConfigTypes = ConfigDescriptor{
		RouteRule,
		IngressRule,
		DestinationPolicy,
	}
)

// istioConfigStore provides a simple adapter for Istio configuration types
// from the generic config registry
type istioConfigStore struct {
	ConfigStore
}

// MakeIstioStore creates a wrapper around a store
func MakeIstioStore(store ConfigStore) IstioConfigStore {
	return &istioConfigStore{store}
}

func (i istioConfigStore) RouteRules() map[string]*proxyconfig.RouteRule {
	out := make(map[string]*proxyconfig.RouteRule)
	rs, err := i.List(RouteRule.Type, "")
	if err != nil {
		glog.V(2).Infof("RouteRules => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Spec.(*proxyconfig.RouteRule); ok {
			out[r.Key()] = rule
		}
	}
	return out
}

func (i *istioConfigStore) RouteRulesBySource(instances []*ServiceInstance) []*proxyconfig.RouteRule {
	type config struct {
		Key  string
		Spec *proxyconfig.RouteRule
	}
	rules := make([]config, 0)
	for key, rule := range i.RouteRules() {
		// validate that rule match predicate applies to source service instances
		if rule.Match != nil && rule.Match.Source != "" {
			found := false
			for _, instance := range instances {
				// must match the source field if it is set
				if rule.Match.Source != instance.Service.Hostname {
					continue
				}
				// must match the tags field - the rule tags are a subset of the instance tags
				var tags Tags = rule.Match.SourceTags
				if tags.SubsetOf(instance.Tags) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		rules = append(rules, config{Key: key, Spec: rule})
	}
	// sort by high precedence first, key string second (keys are unique)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Spec.Precedence > rules[j].Spec.Precedence ||
			(rules[i].Spec.Precedence == rules[j].Spec.Precedence &&
				rules[i].Key < rules[j].Key)
	})

	// project to rules
	out := make([]*proxyconfig.RouteRule, len(rules))
	for i, rule := range rules {
		out[i] = rule.Spec
	}
	return out
}

func (i *istioConfigStore) IngressRules() map[string]*proxyconfig.IngressRule {
	out := make(map[string]*proxyconfig.IngressRule)
	rs, err := i.List(IngressRule.Type, "")
	if err != nil {
		glog.V(2).Infof("IngressRules => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Spec.(*proxyconfig.IngressRule); ok {
			out[r.Key()] = rule
		}
	}
	return out
}

func (i *istioConfigStore) DestinationPolicies() []*proxyconfig.DestinationPolicy {
	out := make([]*proxyconfig.DestinationPolicy, 0)
	rs, err := i.List(DestinationPolicy.Type, "")
	if err != nil {
		glog.V(2).Infof("DestinationPolicies => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Spec.(*proxyconfig.DestinationPolicy); ok {
			out = append(out, rule)
		}
	}
	return out
}

func (i *istioConfigStore) DestinationPolicy(destination string, tags Tags) *proxyconfig.DestinationVersionPolicy {
	names := strings.Split(destination, ".")
	name, namespace := "", ""
	if len(names) > 0 {
		name = names[0]
	}
	if len(names) > 1 {
		namespace = names[1]
	}

	config, exists := i.Get(DestinationPolicy.Type, name, namespace)
	if exists {
		for _, policy := range config.Spec.(*proxyconfig.DestinationPolicy).Policy {
			if tags.Equals(policy.Tags) {
				return policy
			}
		}
	}
	return nil
}
