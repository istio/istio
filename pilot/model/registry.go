// Copyright 2017 Google Inc.
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
	"sort"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

// The Registry describes a set of platform agnostic APIs that must be
// supported by the underlying platform to store and retrieve routing
// rules. The code in proxy/* uses these interfaces to retrieve the routing
// rules pertaining to each service. The exact storage constructs to use
// depends on the platform.  For example, in Kubernetes, one can use the
// ThirdPartyResources to store/retrieve rules.

// The storage registry presented here assumes that the underlying storage
// layer supports GET (list), PUT (add), PATCH (edit) and DELETE semantics

// FIXME rename me to something else for clarity. Registry conflates with
// service registry, while the code here deals only with third party
// resources in kubernetes

// Key is the registry configuration key
type Key struct {
	// Kind specifies the type of configuration
	Kind string
	// Name specifies the unique name per namespace
	Name string
	// Namespace qualifies names
	Namespace string
}

// Registry of the configuration objects
// Object references supplied and returned from this interface should be
// treated as read-only. Modifying them might violate thread-safety.
type Registry interface {
	// Get retrieves a configuration element, bool indicates existence
	Get(key Key) (proto.Message, bool)

	// List returns objects for a kind in a namespace keyed by name
	// Use namespace "" to list all resources across namespaces
	List(kind string, namespace string) (map[Key]proto.Message, error)

	// Put adds an object to the distributed store.
	// This implies that you might not see the effect immediately (e.g. Get
	// might not return the object immediately).
	// Intermittent errors might occur even though the operation succeeds.
	Put(key Key, v proto.Message) error

	// Delete remotes an object from the distributed store.
	// This implies that you might not see the effect immediately (e.g. Get
	// might not return the object immediately).
	// Intermittent errors might occur even though the operation succeeds.
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
}

const (
	// RouteRule kind
	RouteRule = "route-rule"
	// RouteRuleProto message name
	RouteRuleProto = "istio.proxy.v1alpha.config.RouteRule"

	// Destination kind
	Destination = "destination"
	// DestinationProto message name
	DestinationProto = "istio.proxy.v1alpha.config.Destination"
)

var (
	// IstioConfig lists all Istio config kinds
	IstioConfig = KindMap{
		RouteRule: ProtoSchema{
			MessageName: RouteRuleProto,
			Validate:    ValidateRouteRule,
		},
		Destination: ProtoSchema{
			MessageName: DestinationProto,
			Validate:    ValidateDestination,
		},
	}
)

// IstioRegistry provides a simple adapter to edit Istio configuration
type IstioRegistry struct {
	Registry
}

// RouteRules lists all rules in a namespace (or all rules if namespace is "")
func (i *IstioRegistry) RouteRules(namespace string) []*proxyconfig.RouteRule {
	out := make([]*proxyconfig.RouteRule, 0)
	rs, err := i.List(RouteRule, namespace)
	if err != nil {
		glog.V(2).Infof("RouteRules => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.(*proxyconfig.RouteRule); ok {
			out = append(out, rule)
		}
	}
	return out
}

// DestinationRouteRules lists all rules for a destination by precedence
func (i *IstioRegistry) DestinationRouteRules(destination string) []*proxyconfig.RouteRule {
	out := make([]*proxyconfig.RouteRule, 0)
	for _, rule := range i.RouteRules("") {
		if rule.Destination == destination {
			out = append(out, rule)
		}
	}
	sort.Sort(RouteRulePrecedence(out))
	return out
}

// Destinations lists all destination policies in a namespace (or all if namespace is "")
func (i *IstioRegistry) Destinations(namespace string) []*proxyconfig.Destination {
	out := make([]*proxyconfig.Destination, 0)
	rs, err := i.List(Destination, namespace)
	if err != nil {
		glog.V(2).Infof("Destinations => %v", err)
	}
	for _, r := range rs {
		if rule, ok := r.(*proxyconfig.Destination); ok {
			out = append(out, rule)
		}
	}
	return out
}

// RouteRulePrecedence sorts rules by precedence (high precedence first)
type RouteRulePrecedence []*proxyconfig.RouteRule

func (s RouteRulePrecedence) Len() int {
	return len(s)
}

func (s RouteRulePrecedence) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// TODO: define stable order for same precedence
func (s RouteRulePrecedence) Less(i, j int) bool {
	return s[i].Precedence > s[j].Precedence
}
