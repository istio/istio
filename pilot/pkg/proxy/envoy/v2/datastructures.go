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
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	xdscache "github.com/envoyproxy/go-control-plane/pkg/cache"
	xdsserver "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/gogo/protobuf/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util"
)

// Endpoints (react to updates in registry endpoints) - EDS updates
// 1. Endpoints can be shared across multiple clusters.
// 2. The membership of an endpoint in a envoy cluster is based on the set of labels
//    associated with the endpoint and the set of label selectors associated with the envoy cluster.
// 3. When a new endpoint is added, the label selectors associated with the envoy clusters have
//    to be evaluated to determine the envoy clusters to which the endpoint belongs to.
// 4. When an endpoint is deleted, it needs to be removed from all associated envoy clusters.
// 5. When one or more labels are added to an endpoint, the label selectors from envoy clusters
//    that "do not have this endpoint" need to be reevaluated to see if the newly added labels
//    would cause this endpoint to be added to these envoy clusters.
// 6. When one or more labels are "removed" from an endpoint, the envoy clusters currently associated
//    with the endpoint need to be reevaluated to see if the endpoint still qualifies to be a member
//    of these envoy clusters.

// EnvoyClusters (react to updates in services, dest rules, ext services) - CDS updates
// 1. Every service (registry defined, external) in the system has a corresponding cluster in envoy
// 2. Every subset expressed in a DestinationRule will have an associated cluster in envoy.
// 3. EnvoyClusters are added/removed when a service is added/removed or when subsets are
//    added/removed from DestinationRule or when a service is updated (ports are added/removed)
// 4. EnvoyClusters need to be updated when new settings (e.g., circuit breakers, auth) are added
//    via DestinationRules.

// Routes (react to updates in services, route rules) - RDS updates
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

// Listeners (react to updates in services, gateways, ext services) - LDS updates
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
	// name associated with this virtual host
	name string
	// The proto representation of this virtual host
	protoVirtualHost *xdsapi.VirtualHost
	// list of route configurations (RDS responses) that hold this virtual host
	routeConfigurations []*BoxedRouteConfiguration
}

// BoxedRouteConfiguration holds the route configuration proto representation and pointers to
// owning listeners
type BoxedRouteConfiguration struct {
	// Name associated with this route configuration (used in RDS)
	name string
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
	// Unique ID associated with this endpoint
	name string
	// The proto representation of this endpoint
	protoEndpoint *xdsapi.LbEndpoint
	// list of clusters that contain this endpoint
	clusters []*BoxedCluster
	// list of labels associated with this endpoint
	labels []*model.Labels
}

// BoxedClusterConfig holds the envoy cluster proto representations
type BoxedCluster struct {
	// name associated with this cluster
	name string
	// The proto representation of this envoy cluster
	protoEnvoyCluster *xdsapi.Cluster
	// The proto representation of the EDS data for this envoy cluster
	protoEDSresp *xdsapi.ClusterLoadAssignment
	// list of endpoints associated with this envoy cluster
	endpoints []*BoxedEndpoint
	// List of labels associated with this envoy cluster
	labels []*model.Labels
}

// Indices is a set of reverse indices to quickly locate endpoints, clusters, routes
// and listeners
type Indices struct {
	// A Mutex to guard the entire Index.
	// Where should it be used? TBD
	indicesMutex sync.RWMutex

	// A Mutex to guard listener table
	listenerMutex sync.RWMutex

	// A Mutex to guard cluster and endpoints
	clusterMutex sync.RWMutex

	// A mutex to guard route objects (virtual hosts, route configs)
	rdsMutex sync.RWMutex

	// Map of listeners across the system indexed by port
	listeners map[int]*xdsapi.Listener

	// Map of virtual hosts indexed by name (not DNS). When a route rule has source based/gateway
	// based match, the virtual host name has to be tweaked accordingly.
	virtualHosts map[string]*BoxedVirtualHost

	// Map of route configurations (used by RDS in each listener) indexed by name.
	routeConfigs map[string]*BoxedRouteConfiguration

	// A mutex to guard CDS
	// Map of envoy clusters indexed by name
	envoyClusters map[string]*BoxedCluster

	// Map of UID to the network endpoint. The UID is expected to be unique across the Mesh.
	endpoints map[string]*BoxedEndpoint

	// Map of a "single" label string ("key=value") to the list of envoy clusters that use this label.
	labelToEnvoyClusters map[string][]*BoxedCluster
}

// EnvoyInstance represents information associated with a single Envoy sidecar
type EnvoyInstance struct{}

// Hash returns a globally unique ID for a given Envoy node
// This implements the interface needed by xds cache
func (e EnvoyInstance) Hash(node *xdsapi.Node) (xdscache.Key, error) {
	// DUMMY IMPLEMENTATION
	return xdscache.Key(node.Id), nil
}

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	model.Environment
	webhookClient    *http.Client
	webhookEndpoint  string
	indices          *Indices
	envoyConfigCache *xdscache.SimpleCache
}

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	Port            int
	MonitoringPort  int
	EnableProfiling bool
	EnableCaching   bool
	WebhookEndpoint string
}

// Start ADS server on a given port
// xdscache.Cache is a simple cache that holds the entire config for each envoy connected to pilot
func startADS(ctx context.Context, envoyConfigCache xdscache.Cache, port int) {
	server := xdsserver.NewServer(envoyConfigCache)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		zap.Logger{}.Fatal("failed to listen:", zap.Error(err))
	}

	xdsapi.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	zap.Logger{}.Info("ADS server listening on ", zap.Int("port", port))

	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			zap.Logger{}.Error("ADS error:", zap.Error(err))
		}
	}()
	<-ctx.Done()
	grpcServer.GracefulStop()
}

// UpdateIndices updates the indices in Pilot in response to a change in service registry, or route rules,
// and pushes out updates to all the envoys connected to this pilot. Once we add/remove/update clusters,
// listeners, etc., we call the SetSnapshot function of the envoyConfigCache. Changing the cache snapshot
// for a node will cause the ADS server to push the updated listeners, clusters, etc. to the respective envoy.
// The ADS server implementation should take care of pushing the resources in the correct order (i.e. clusters,
// then endpoints, then listeners
func (ds *DiscoveryService) UpdateIndices(svc *model.Service, instance *model.ServiceInstance,
	rule *model.Config, e model.Event) {
	// Update listeners, associated clusters, etc.
	// by calling all the buildXYZ functions
	indices := ds.indices
	// dummy values
	indices.listeners[80] = &xdsapi.Listener{}
	indices.envoyClusters["port-80-service-foo-cluster"] = &BoxedCluster{}

	// This is pretty bad (copy by value) but the snapshot interface requires values
	// That interface needs to be tweaked
	clusterLoadAssignments := make([]proto.Message, len(indices.envoyClusters))
	clusters := make([]proto.Message, len(indices.envoyClusters))
	routes := make([]proto.Message, len(indices.routeConfigs))
	listeners := make([]proto.Message, len(indices.listeners))

	// The code below assumes that everyone has same set of clusters, listeners, etc.
	// This would have to be tweaked to populate cluster array based on source match rules in route rules
	for _, c := range indices.envoyClusters {
		clusterLoadAssignments = append(clusterLoadAssignments, proto.Message(c.protoEDSresp))
		clusters = append(clusters, proto.Message(c.protoEnvoyCluster))
	}

	for _, r := range indices.routeConfigs {
		routes = append(routes, proto.Message(r.protoRouteConfiguration))
	}

	for _, l := range indices.listeners {
		listeners = append(listeners, proto.Message(l))
	}

	// Schedule updates to nodes
	// In common case, we might need to update all endpoints when a new cluster/service is added
	for _, e := range indices.endpoints {
		// OPTIMIZE..
		snapshot := xdscache.NewSnapshot("unique-version-string",
			clusterLoadAssignments, clusters, routes, listeners)
		ds.envoyConfigCache.SetSnapshot(xdscache.Key(e.name), snapshot)
	}
}

// NewDiscoveryService creates an Envoy discovery service on a given port
func NewDiscoveryService(ctx context.Context, ctl model.Controller, configCache model.ConfigStoreCache,
	environment model.Environment, o DiscoveryServiceOptions) (*DiscoveryService, error) {
	discoveryService := &DiscoveryService{
		Environment: environment,
		indices: &Indices{
			listeners:     make(map[int]*xdsapi.Listener),
			virtualHosts:  make(map[string]*BoxedVirtualHost),
			routeConfigs:  make(map[string]*BoxedRouteConfiguration),
			envoyClusters: make(map[string]*BoxedCluster),
			endpoints:     make(map[string]*BoxedEndpoint),
		},
		envoyConfigCache: xdscache.NewSimpleCache(EnvoyInstance{}, nil),
	}

	discoveryService.webhookEndpoint, discoveryService.webhookClient = util.NewWebHookClient(o.WebhookEndpoint)

	// set up the rest/grpc server
	startADS(ctx, discoveryService.envoyConfigCache, o.Port)

	// The service model provides two callbacks
	// 1. the ServiceChange callback notifies us of updates to the service object (add/remove/new labels/ports)
	// 2. the EndpointChange notifies us of updates to the instances of a service (add/remove or label changes)
	serviceHandler := func(s *model.Service, e model.Event) { discoveryService.UpdateIndices(s, nil, nil, e) }
	instanceHandler := func(i *model.ServiceInstance, e model.Event) { discoveryService.UpdateIndices(nil, i, nil, e) }

	if err := ctl.ServiceChange(serviceHandler); err != nil {
		return nil, err
	}
	if err := ctl.EndpointChange(instanceHandler); err != nil {
		return nil, err
	}

	// Config handler callback is invoked when routing rules change
	configHandler := func(rule model.Config, e model.Event) { discoveryService.UpdateIndices(nil, nil, rule, e) }
	for _, descriptor := range model.IstioConfigTypes {
		// register a callback for route rules, destination rules, external services, gateways, etc.
		configCache.RegisterEventHandler(descriptor.Type, configHandler)
	}

	return discoveryService, nil
}
