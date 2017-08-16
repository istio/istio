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
	"sort"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model/test"
)

// Config is a configuration unit consisting of the type of configuration, the
// key identifier that is unique per type, and the content represented as a
// protobuf message.  The revision is optional, and if provided, identifies the
// last update operation on the object.
type Config struct {
	// Type is a short configuration name that matches the content message type
	Type string

	// Key is the type-dependent unique identifier for this config object, derived
	// from its content
	Key string

	// Revision is an opaque identifier for tracking updates to the config registry.
	// The implementation may use a change index or a commit log for the revision.
	// The config client should not make any assumptions about revisions and rely only on
	// exact equality to implement optimistic concurrency of read-write operations.
	//
	// The lifetime of an object of a particular revision depends on the underlying data store.
	// The data store may compactify old revisions in the interest of storage optimization.
	//
	// An empty revision carries a special meaning that the associated object has
	// not been stored and assigned a revision.
	Revision string

	// Content holds the configuration object as a protobuf message
	Content proto.Message
}

// ConfigStore describes a set of platform agnostic APIs that must be supported
// by the underlying platform to store and retrieve Istio configuration.
//
// The storage interface presented here assumes that the underlying storage
// layer supports _GET_ (list), _PUT_ (update), _POST_ (create) and _DELETE_
// semantics but does not guarantee any transactional semantics.
//
// Configuration objects possess a type property and a key property. The
// configuration key is derived from the content of the config object and
// uniquely identifies objects for a particular type. For example, if the
// object schema contains metadata fields _name_ and _namespace_, the
// configuration key may be defined as _namespace/name_.  Key definition is
// provided as part of the config type definition.
//
// _PUT_, _POST_, and _DELETE_ are mutator operations. These operations are
// asynchronous, and you might not see the effect immediately (e.g. _GET_ might
// not return the object by key immediately after you mutate the store.)
// Intermittent errors might occur even though the operation succeeds, so you
// should always check if the object store has been modified even if the
// mutating operation returns an error.  Objects should be created with _POST_
// operation and updated with _PUT_ operation.
//
// Revisions record the last mutation operation on each object. If a mutation
// is applied to a different revision of an object than what the underlying
// storage expects as defined by pure equality, the operation is blocked.  The
// client of this interface should not make assumptions about the structure or
// ordering of the revision identifier.
//
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them violates thread-safety.
type ConfigStore interface {
	// ConfigDescriptor exposes the configuration type schema known by the config store.
	// The type schema defines the bidrectional mapping between configuration
	// types and the protobuf encoding schema.
	ConfigDescriptor() ConfigDescriptor

	// Get retrieves a configuration element by a type and a key
	Get(typ, key string) (config proto.Message, exists bool, revision string)

	// List returns objects by type
	List(typ string) ([]Config, error)

	// Post creates a configuration object. If an object with the same key for
	// the type already exists, the operation fails with no side effects.
	Post(config proto.Message) (revision string, err error)

	// Put updates a configuration object in the store.  Put requires that the
	// object has been created.  Revision prevents overriding a value that has
	// been changed between prior _Get_ and _Put_ operation to achieve optimistic
	// concurrency. This method returns a new revision if the operation succeeds.
	Put(config proto.Message, oldRevision string) (newRevision string, err error)

	// Delete removes an object from the store by key
	Delete(typ, key string) error
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

	// Key function derives the unique key from the configuration object metadata
	// properties. The object is not required to be complete or even valid to
	// derive its key, but must be an instance of the expected message type. For
	// example, if _name_ field is designated as the key, then the proto message
	// must only possess _name_ property.
	Key func(config proto.Message) string
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
		Key: func(config proto.Message) string {
			return config.(*test.MockConfig).Key
		},
	}

	// RouteRule describes route rules
	RouteRule = ProtoSchema{
		Type:        "route-rule",
		Plural:      "route-rules",
		MessageName: "istio.proxy.v1.config.RouteRule",
		Validate:    ValidateRouteRule,
		Key: func(config proto.Message) string {
			rule := config.(*proxyconfig.RouteRule)
			return rule.Name
		},
	}

	// IngressRule describes ingress rules
	IngressRule = ProtoSchema{
		Type:        "ingress-rule",
		Plural:      "ingress-rules",
		MessageName: "istio.proxy.v1.config.IngressRule",
		Validate:    ValidateIngressRule,
		Key: func(config proto.Message) string {
			rule := config.(*proxyconfig.IngressRule)
			return rule.Name
		},
	}

	// DestinationPolicy describes destination rules
	DestinationPolicy = ProtoSchema{
		Type:        "destination-policy",
		Plural:      "destination-policies",
		MessageName: "istio.proxy.v1.config.DestinationPolicy",
		Validate:    ValidateDestinationPolicy,
		Key: func(config proto.Message) string {
			return config.(*proxyconfig.DestinationPolicy).Destination
		},
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
	rs, err := i.List(RouteRule.Type)
	if err != nil {
		glog.V(2).Infof("RouteRules => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Content.(*proxyconfig.RouteRule); ok {
			out[r.Key] = rule
		}
	}
	return out
}

func (i *istioConfigStore) RouteRulesBySource(instances []*ServiceInstance) []*proxyconfig.RouteRule {
	rules := make([]Config, 0)
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
		rules = append(rules, Config{Key: key, Content: rule})
	}
	// sort by high precedence first, key string second (keys are unique)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Content.(*proxyconfig.RouteRule).Precedence > rules[j].Content.(*proxyconfig.RouteRule).Precedence ||
			(rules[i].Content.(*proxyconfig.RouteRule).Precedence == rules[j].Content.(*proxyconfig.RouteRule).Precedence &&
				rules[i].Key < rules[j].Key)
	})

	// project to rules
	out := make([]*proxyconfig.RouteRule, len(rules))
	for i, rule := range rules {
		out[i] = rule.Content.(*proxyconfig.RouteRule)
	}
	return out
}

func (i *istioConfigStore) IngressRules() map[string]*proxyconfig.IngressRule {
	out := make(map[string]*proxyconfig.IngressRule)
	rs, err := i.List(IngressRule.Type)
	if err != nil {
		glog.V(2).Infof("IngressRules => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Content.(*proxyconfig.IngressRule); ok {
			out[r.Key] = rule
		}
	}
	return out
}

func (i *istioConfigStore) DestinationPolicies() []*proxyconfig.DestinationPolicy {
	out := make([]*proxyconfig.DestinationPolicy, 0)
	rs, err := i.List(DestinationPolicy.Type)
	if err != nil {
		glog.V(2).Infof("DestinationPolicies => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.Content.(*proxyconfig.DestinationPolicy); ok {
			out = append(out, rule)
		}
	}
	return out
}

func (i *istioConfigStore) DestinationPolicy(destination string, tags Tags) *proxyconfig.DestinationVersionPolicy {
	value, exists, _ := i.Get(DestinationPolicy.Type, destination)
	if exists {
		for _, policy := range value.(*proxyconfig.DestinationPolicy).Policy {
			if tags.Equals(policy.Tags) {
				return policy
			}
		}
	}
	return nil
}
