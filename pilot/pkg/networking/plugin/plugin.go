// Copyright 2018 Istio Authors
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

package plugin

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"istio.io/istio/pilot/pkg/model"
)

// ListenerType is the type of listener.
type ListenerType int

const (
	// ListenerTypeTCP is a TCP listener.
	ListenerTypeTCP = iota
	// ListenerTypeHTTP is an HTTP listener.
	ListenerTypeHTTP
)

// CallbackListenerInputParams is a set of values passed to On*Listener callbacks. Not all fields are guaranteed to
// be set, it's up to the callee to validate required fields are set and emit error if they are not.
// These are for reading only and should not be modified.
type CallbackListenerInputParams struct {
	// ListenerType is the type of listener (TCP, HTTP etc.)
	ListenerType ListenerType
	// Env is the model environment.
	Env *model.Environment
	// Node is the node the listener is for.
	Node *model.Proxy
	// ProxyInstances is a slice of all proxy service instances in the mesh.
	ProxyInstances []*model.ServiceInstance
	// ServiceInstance is the service instance colocated with the listener (applies to sidecar).
	ServiceInstance *model.ServiceInstance
}

// CallbackListenerMutableObjects is a set of objects passed to On*Listener callbacks. Fields may be nil or empty.
// Any lists should not be overridden, but rather only appended to.
// Non-list fields may be mutated; however it's not recommended to do this since it can affect other plugins in the
// chain in unpredictable ways.
type CallbackListenerMutableObjects struct {
	// HTTPFilters is the slice of HTTP filters for the Listener. Append to only.
	HTTPFilters []*http_conn.HttpFilter
	// TCPFilters is the slice of TCP filters for the Listener. Append to only.
	TCPFilters []listener.Filter
	// Listener is the listener being built. Any field may be modified, but changing other than appending to slices
	// is discouraged.
	Listener *xdsapi.Listener
}

// ListenerPlugin is a plugin called during the construction of a xdsapi.Listener which may alter the Listener in any
// way. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, the mixer plugin that sets up policy checks on the inbound listener, etc.
type ListenerPlugin interface {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
	// Can be used to add additional filters on the outbound path.
	OnOutboundListener(in *CallbackListenerInputParams, mutable *CallbackListenerMutableObjects) error

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters.
	OnInboundListener(in *CallbackListenerInputParams, mutable *CallbackListenerMutableObjects) error

	// OnOutboundCluster is called whenever a new cluster is added to the CDS output
	// Typically used by AuthN plugin to add mTLS settings
	OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
		cluster *xdsapi.Cluster)

	// OnInboundCluster is called whenever a new cluster is added to the CDS output
	// Not used typically
	OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
		cluster *xdsapi.Cluster)

	// OnOutboundHttpRoute is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is
	// added to RDS in the outbound path. Can be used to add route specific metadata or additional headers to forward
	OnOutboundRoute(env model.Environment, node model.Proxy, route *xdsapi.RouteConfiguration)

	// OnInboundRoute is called whenever a new set of virtual hosts are added to the inbound path.
	// Can be used to enable route specific stuff like Lua filters or other metadata.
	OnInboundRoute(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
		route *xdsapi.RouteConfiguration)
}
