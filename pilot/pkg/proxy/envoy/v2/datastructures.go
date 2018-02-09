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


package v2

import (
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	"istio.io/istio/pilot/pkg/model"
)

// Endpoints (react to updates in registry endpoints) (notify clusters)
// 1. Endpoints can be shared across multiple clusters.
// 2. The membership of an endpoint in a cluster is based on the set of labels
//    associated with the endpoint and the set of label selectors associated with the cluster.
// 3. When a new endpoint is added, the label selectors associated with the clusters have
//    to be evaluated to determine the clusters to which the endpoint belongs to.
// 4. When an endpoint is deleted, it needs to be removed from all associated clusters.
// 5. When one or more labels are added to an endpoint, the label selectors from clusters
//    that "do not have this endpoint" need to be reevaluated to see if the newly added labels
//    would cause this endpoint to be added to these clusters.
// 6. When one or more labels are "removed" from an endpoint, the clusters currently associated
//    with the endpoint need to be reevaluated to see if the endpoint still qualifies to be a member
//    of these clusters.

// Clusters (react to updates in services, dest rules, ext services)
// 1. Every service (registry defined, external) in the system has a corresponding cluster
// 2. Every subset expressed in a DestinationRule will have an associated cluster.
// 3. Clusters are added/removed when a service is added/removed or when subsets are
//    added/removed from DestinationRule or when a service is updated (ports are added/removed)
// 4. Clusters need to be updated when new settings (e.g., circuit breakers, auth) are added
//    via DestinationRules.

// Routes (react to updates in services, route rules)
// 1. Envoy RDS response is essentially an array of virtual hosts on a port or TCP routes on a port.
// 2. Every HTTP service in the system will have a virtual host and atleast a default route (/)
// 3. The HTTP service could have additional routes based on route rules.
// 4. Every TCP service in the system will have a TCPRoute (or a filter chain match) associated
//    with its service IP if present.
// 5. If a TCP service has no service IP, it will have a listener on the service port and a
//    catch all TCPRoute
// 6. Every HTTP port of a service will share the same route configuration (list of virtual hosts)
//    until further delineation is created by route rules.
// 7. Similarly, every TCP port of a service will share the same TCPRoute.
// 7. Routes change (add/delete/update) typically when route rules change
// 9. Routes can also change when ports are added/removed from the service

// Listeners (react to updates in services, gateways, ext services)
// 1. Every unique port in the system will have an associated listener.
// 2. A service can generate one or more listeners based on the number of ports it listens on
// 3. New listeners are added when a service (incl gateway/external service) with unique
//    (previously unused) set of ports are added.
// 4. Listeners are updated when one or more services/gateways/extsvs are added/removed to/from
//    the same listener port.
// 5. Listeners are removed when there are no services/gateways/extsvcs listening on the listener
//    port.

// The indices here are doubly linked. A pair of objects are dependent on each other if they have
// to be updated when one changes.

// BoxedVirtualHost holds the virtual host proto representation and pointers to
// owning route configurations.
type BoxedVirtualHost struct {
	// The proto representation of this virtual host
	protoVirtualHost *xdsapi.VirtualHost
	// list of route configurations (RDS responses) that hold this virtual host
	routeConfigurations []*BoxedRouteConfiguration
}

// BoxedRouteConfiguration holds the route configuration proto representation and pointers to
// owning listeners
type BoxedRouteConfiguration struct {
	// The proto representation of this route configuration
	protoRouteConfiguration *xdsapi.RouteConfiguration
	// list of virtual hosts attached to this route configuration
	virtualHosts []*BoxedVirtualHost
	// list of listeners that use this route configuration
	listeners []*xdsapi.Listener
}

// BoxedEndpoint holds the endpoint proto representation and pointers to
// owning clusters
type BoxedEndpoint struct {
	// The proto representation of this endpoint
	protoEndpoint *xdsapi.LbEndpoint
	// list of clusters that contain this endpoint
	clusters []*BoxedCluster
	// list of labels associated with this endpoint
	labels []*model.Labels
}

// BoxedCluster holds the cluster proto representation, pointers to endpoints
type BoxedCluster struct {
	// The proto representation of this cluster
	protoCluster *xdsapi.Cluster
	// list of endpoints associated with this cluster
	endpoints []*BoxedEndpoint
	// List of labels associated with this cluster
	labels []*model.Labels
}

// Indices is a set of reverse indices to quickly locate endpoints, clusters, routes
// and listeners
type Indices struct {
	// Mutex guards guarantees consistency of updates to members shared across threads.
	mu sync.RWMutex

	// Map of listeners across the system indexed by port
	listeners map[int]*xdsapi.Listener

	// Map of virtual hosts indexed by name (not DNS). When a route rule has source based/gateway
	// based match, the virtual host name has to be tweaked accordingly.
	virtualHosts map[string]*BoxedVirtualHost

	// Map of route configurations (used by RDS in each listener) indexed by name.
	routeConfigs map[string]*BoxedRouteConfiguration

	// Map of clusters indexed by name
	clusters map[string]*BoxedCluster

	// Map of UID to the network endpoint. The UID is expected to be unique across the Mesh.
	endpoints map[string]*BoxedEndpoint

	// Map of a "single" label string ("key=value") to the list of clusters that use this label.
	labelToClusters map[string][]*BoxedCluster
}
