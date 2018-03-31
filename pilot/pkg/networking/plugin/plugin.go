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

	"istio.io/istio/pilot/pkg/model"
)

// Callbacks represents the interfaces implemented by code that modifies the default output of
// networking. Examples include AuthenticationPlugin that sets up mTLS authentication on the inbound Listener
// and outbound Cluster, the mixer plugin that sets up policy checks on the inbound listener, etc.
type Callbacks interface {
	// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
	// Can be used to add additional filters on the outbound path
	OnOutboundListener(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
		listener *xdsapi.Listener)

	// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
	// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
	// on the inbound path
	OnInboundListener(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
		listener *xdsapi.Listener)

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
