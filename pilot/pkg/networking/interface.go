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

package networking

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"istio.io/istio/pilot/pkg/model"
)

// Networking represents the interfaces to be implemented by code that generates xDS responses
type Networking struct {
	// BuildListeners returns the list of listeners for the given proxy. This is the LDS output
	// Internally, the computation will be optimized to ensure that listeners are computed only
	// once and shared across multiple invocations of this function.
	BuildListeners func(env model.Environment, node model.Proxy) []*v2.Listener

	// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
	BuildClusters func(env model.Environment, node model.Proxy) []*v2.Cluster

	// BuildRoutes returns the list of routes for the given proxy. This is the RDS output
	BuildRoutes func(env model.Environment, node model.Proxy, routeName string) []*v2.RouteConfiguration
}

// PluginCallbacks represents the interfaces implemented by code that modifies the default output of
// networking. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, the mixer plugin that sets up policy checks on the inbound listener, etc.
// TODO: determine the function params after looking at use cases
type PluginCallbacks struct {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
	// Can be used to add additional filters on the outbound path
	OnOutboundListener func(env model.Environment, node model.Proxy, service *model.Service, listener *v2.Listener) *v2.Listener

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
	// on the inbound path
	OnInboundListener func(env model.Environment, node model.Proxy, service *model.Service, listener *v2.Listener) *v2.Listener

	// OnOutboundCluster is called whenever a new cluster is added to the CDS output
	// Typically used by AuthN plugin to add mTLS settings
	OnOutboundCluster func(env model.Environment, node model.Proxy, service *model.Service, cluster *v2.Cluster) *v2.Cluster

	// OnInboundCluster is called whenever a new cluster is added to the CDS output
	// Not used typically
	OnInboundCluster func(env model.Environment, node model.Proxy, service *model.Service, cluster *v2.Cluster) *v2.Cluster

	// OnOutboundHttpRoute is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is added to
	// RDS in the outbound path. Can be used to add route specific metadata or additional headers to forward
	OnOutboundRoute func(env model.Environment, node model.Proxy, service model.Service, route *v2.RouteConfiguration) *v2.RouteConfiguration

	// OnInboundRoute is called whenever a new set of virtual hosts are added to the inbound path.
	// Can be used to enable route specific stuff like Lua filters or other metadata.
	OnInboundRoute func(env model.Environment, node model.Proxy, service model.Service, route *v2.RouteConfiguration) *v2.RouteConfiguration
}

// NewDataplane creates a new instance of dataplane configuration generator
func NewDataplane() *Networking {
	//dataplane := v1alpha3.NewDataplane() // can be swapped with other versions in future
	//return dataplane
	return nil
}

// NewPlugins returns a list of plugin instance handles. Each plugin implements the PluginCallbacks interfaces
func NewPlugins() []*PluginCallbacks {
	plugins := make([]*PluginCallbacks, 0)
	// plugins = append(plugins, NewAuthNPlugin())
	// plugins = append(plugins, NewMixerPlugin())
	// plugins = append(plugins, NewAPImPlugin())
	return plugins
}
