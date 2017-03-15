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
	"fmt"
	"sort"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

// Config Registry describes a set of platform agnostic APIs that must be
// supported by the underlying platform to store and retrieve Istio configuration.
//
// The storage registry presented here assumes that the underlying storage
// layer supports GET (list), PUT (add), PATCH (edit) and DELETE semantics
// but does not guarantee any transactional semantics.

// Key is the registry configuration key
type Key struct {
	// Kind specifies the type of configuration
	Kind string
	// Name specifies the unique name per namespace
	Name string
	// Namespace qualifies names
	Namespace string
}

// String returns a human-readable textual representation of a key
func (k Key) String() string {
	return fmt.Sprintf("%s/%s-%s", k.Namespace, k.Kind, k.Name)
}

// ConfigRegistry defines the basic API for retrieving and storing
// configuration artifacts.
//
// "Put", "Post", and "Delete" are mutator operations. These operations are
// asynchronous, and you might not see the effect immediately (e.g. "Get" might
// not return the object by key immediately after you mutate the store.)
// Intermittent errors might occur even though the operation succeeds, so you
// should always check if the object store has been modified even if the
// mutating operation returns an error.
//
// Objects should be created with "Post" operation and updated with "Put" operation.
//
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them violates thread-safety.
type ConfigRegistry interface {
	// Get retrieves a configuration element, bool indicates existence
	Get(key Key) (proto.Message, bool)

	// List returns objects for a kind in a namespace.
	// Use namespace "" to list resources across namespaces
	List(kind string, namespace string) (map[Key]proto.Message, error)

	// Post creates a configuration object. If an object with the same
	// key already exists, the operation fails with no side effects.
	Post(key Key, v proto.Message) error

	// Put updates a configuration object in the distributed store.
	// Put requires that the object has been created.
	Put(key Key, v proto.Message) error

	// Delete removes an object from the distributed store by key.
	Delete(key Key) error
}

// KindMap defines bijection between Kind name and proto message name
type KindMap map[string]ProtoSchema

// ProtoSchema provides custom validation checks
type ProtoSchema struct {
	// MessageName refers to the protobuf message type name
	MessageName string
	// Validate configuration as a protobuf message
	Validate func(o proto.Message) error
	// Internal flag indicates that the configuration type is derived
	// from other configuration sources. This prohibits direct updates
	// but allows listing and watching.
	Internal bool
}

// Kinds lists all kinds in the kind schemas
func (km KindMap) Kinds() []string {
	kinds := make([]string, 0)
	for kind := range km {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

const (
	// RouteRule defines the kind for the route rule configuration
	RouteRule = "route-rule"
	// RouteRuleProto message name
	RouteRuleProto = "istio.proxy.v1alpha.config.RouteRule"

	// IngressRule kind
	IngressRule = "ingress-rule"
	// IngressRuleProto message name
	IngressRuleProto = RouteRuleProto

	// DestinationPolicy defines the kind for the destination policy configuration
	DestinationPolicy = "destination-policy"
	// DestinationPolicyProto message name
	DestinationPolicyProto = "istio.proxy.v1alpha.config.DestinationPolicy"
)

var (
	// IstioConfig lists all Istio config kinds with schemas and validation
	IstioConfig = KindMap{
		RouteRule: ProtoSchema{
			MessageName: RouteRuleProto,
			Validate:    ValidateRouteRule,
		},
		IngressRule: ProtoSchema{
			MessageName: IngressRuleProto,
			Validate:    ValidateIngressRule,
			Internal:    true,
		},
		DestinationPolicy: ProtoSchema{
			MessageName: DestinationPolicyProto,
			Validate:    ValidateDestinationPolicy,
		},
	}
)

// IstioRegistry provides a simple adapter for Istio configuration kinds
type IstioRegistry struct {
	ConfigRegistry
}

// RouteRules lists all routing rules in a namespace (or all rules if namespace is "")
func (i *IstioRegistry) RouteRules(namespace string) map[Key]*proxyconfig.RouteRule {
	out := make(map[Key]*proxyconfig.RouteRule)
	rs, err := i.List(RouteRule, namespace)
	if err != nil {
		glog.V(2).Infof("RouteRules => %v", err)
	}
	for key, r := range rs {
		if rule, ok := r.(*proxyconfig.RouteRule); ok {
			out[key] = rule
		}
	}
	return out
}

type routeRuleConfig struct {
	Key
	rule *proxyconfig.RouteRule
}

// RouteRulesBySource selects routing rules by source service instances.
// The rules are sorted by precedence (high first) in a stable manner.
func (i *IstioRegistry) RouteRulesBySource(namespace string, instances []*ServiceInstance) []*proxyconfig.RouteRule {
	rules := make([]*routeRuleConfig, 0)
	for key, rule := range i.RouteRules(namespace) {
		// validate that rule match predicate applies to source service instances
		if rule.Match != nil {
			found := false
			for _, instance := range instances {
				if rule.Match.Source != "" && rule.Match.Source != instance.Service.Hostname {
					continue
				}
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
		rules = append(rules, &routeRuleConfig{Key: key, rule: rule})
	}
	// sort by high precedence first, key string second (keys are unique)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].rule.Precedence > rules[j].rule.Precedence ||
			(rules[i].rule.Precedence == rules[j].rule.Precedence && rules[i].Key.String() < rules[j].Key.String())
	})

	// project to rules
	out := make([]*proxyconfig.RouteRule, len(rules))
	for i, rule := range rules {
		out[i] = rule.rule
	}
	return out
}

// IngressRules lists all ingress rules in a namespace (or all rules if namespace is "")
func (i *IstioRegistry) IngressRules(namespace string) map[Key]*proxyconfig.RouteRule {
	out := make(map[Key]*proxyconfig.RouteRule)
	rs, err := i.List(IngressRule, namespace)
	if err != nil {
		glog.V(2).Infof("IngressRules => %v", err)
	}
	for key, r := range rs {
		if rule, ok := r.(*proxyconfig.RouteRule); ok {
			out[key] = rule
		}
	}
	return out
}

// PoliciesByNamespace lists all destination policies in a namespace (or all if namespace is "")
func (i *IstioRegistry) PoliciesByNamespace(namespace string) []*proxyconfig.DestinationPolicy {
	out := make([]*proxyconfig.DestinationPolicy, 0)
	rs, err := i.List(DestinationPolicy, namespace)
	if err != nil {
		glog.V(2).Infof("DestinationPolicies => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.(*proxyconfig.DestinationPolicy); ok {
			out = append(out, rule)
		}
	}
	return out
}

// DestinationPolicies lists all policies for a service version.
// Policies are not inherited by tags inclusion. The policy tags must match the tags precisely.
func (i *IstioRegistry) DestinationPolicies(destination string, tags Tags) []*proxyconfig.DestinationPolicy {
	out := make([]*proxyconfig.DestinationPolicy, 0)
	for _, value := range i.PoliciesByNamespace("") {
		if value.Destination == destination && tags.Equals(value.Tags) {
			out = append(out, value)
		}
	}
	// TODO: sort destination policies
	return out
}
