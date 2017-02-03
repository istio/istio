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
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

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
	List(kind string, namespace string) (map[string]proto.Message, error)

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

	// UpstreamCluster kind
	UpstreamCluster = "upstream-cluster"
	// UpstreamClusterProto message name
	UpstreamClusterProto = "istio.proxy.v1alpha.config.UpstreamCluster"
)

var (
	// IstioConfig lists all Istio config kinds
	IstioConfig = KindMap{
		RouteRule: ProtoSchema{
			MessageName: RouteRuleProto,
			Validate:    ValidateRouteRule,
		},
		UpstreamCluster: ProtoSchema{
			MessageName: UpstreamClusterProto,
			Validate:    ValidateUpstreamCluster,
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
