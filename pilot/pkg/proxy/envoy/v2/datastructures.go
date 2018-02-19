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
	"sort"
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	xdscache "github.com/envoyproxy/go-control-plane/pkg/cache"
	xdsserver "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/gogo/protobuf/proto"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util"
)

// Endpoints (react to updates in registry endpoints) - EDS updates
// stored in endpoints map[string]*EnvoyEndpoint
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
// stored in envoyClusters map[string]*EnvoyCluster
// 1. Every service (registry defined, external) in the system has a corresponding cluster in envoy
// 2. Every subset expressed in a DestinationRule will have an associated cluster in envoy.
// 3. EnvoyClusters are added/removed when a service is added/removed or when subsets are
//    added/removed from DestinationRule or when a service is updated (ports are added/removed)
// 4. EnvoyClusters need to be updated when new settings (e.g., circuit breakers, auth) are added
//    via DestinationRules.

// Routes (react to updates in services, route rules) - RDS updates
// stored in routeConfigs map[string]*RouteConfiguration
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
// stored in listeners map[string]*xdsapi.Listeners
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

// VirtualHost holds the virtual host proto representation and pointers to
// owning route configurations.
type VirtualHost struct {
	// name associated with this virtual host
	name string
	// The proto representation of this virtual host
	protoVirtualHost *xdsapi.VirtualHost
	// list of route configurations (RDS responses) that hold this virtual host
	// use: When a virtual host changes, we have to update the protos of *all* associated
	// route configurations, and push these to envoys
	routeConfigurations []*RouteConfiguration
}

// RouteConfiguration holds the route configuration proto representation and pointers to
// owning listeners
type RouteConfiguration struct {
	// Name associated with this route configuration (used in RDS)
	name string
	// The proto representation of this route configuration
	protoRouteConfiguration *xdsapi.RouteConfiguration
	// list of virtual hosts attached to this route configuration
	// use: When a route configuration is deleted, we need to update all the
	// constituent virtual hosts - wherein we update the backpointers to *not* point to
	// this route configuration anymore (essentially removing references).
	virtualHosts []*VirtualHost
}

// EnvoyEndpoint holds the endpoint proto representation and pointers to
// owning clusters
type EnvoyEndpoint struct {
	// Unique ID associated with this endpoint
	name string
	// The proto representation of this endpoint
	protoEndpoint *xdsapi.LbEndpoint
	// list of clusters that contain this endpoint
	// use: when an endpoint is removed or its labels change, we use this
	// slice to update *all* clusters that contain this endpoint.
	clusters []*EnvoyCluster
	// map of labels associated with this endpoint
	labels map[string]string
}

// EnvoyCluster holds the envoy cluster proto representations
type EnvoyCluster struct {
	// name associated with this cluster
	name string
	// The proto representation of this envoy cluster
	protoEnvoyCluster *xdsapi.Cluster
	// The proto representation of the EDS data for this envoy cluster
	protoEDSresp *xdsapi.ClusterLoadAssignment
	// list of endpoints associated with this envoy cluster
	// use: when a cluster is removed, use this list to update
	// the back references of all associated endpoints. When a destination rule
	// subset changes (e.g., label removed), iterate over all endpoints in this list
	// to see if any need to be removed. Note that its more common to create/delete subsets
	// than to add/remove labels from a subset.
	endpoints []*EnvoyEndpoint
	// map of labels associated with this envoy cluster
	labels map[string]string
}

// Indices is a set of reverse indices to quickly locate endpoints, clusters, routes
// and listeners
type Indices struct {
	// A Mutex to guard the entire Index.
	indicesMutex sync.RWMutex

	// Map of listeners across the system indexed by port
	listeners map[int]*xdsapi.Listener

	// Map of virtual hosts indexed by name (not DNS). When a route rule has source based/gateway
	// based match, the virtual host name has to be tweaked accordingly.
	virtualHosts map[string]*VirtualHost

	// Map of route configurations (used by RDS in each listener) indexed by name.
	routeConfigs map[string]*RouteConfiguration

	// A mutex to guard CDS
	// Map of envoy clusters indexed by name
	envoyClusters map[string]*EnvoyCluster

	// Map of UID to the network endpoint. The UID is expected to be unique across the Mesh.
	// Note that some endpoints in this map may not be in the service registry (e.g., envoy nodes
	// representing a dedicated pool of gateways)
	endpoints map[string]*EnvoyEndpoint

	// Map of a "single" label string ("key=value") to the list of envoy clusters that use this label.
	labelToEnvoyClusters map[string][]*EnvoyCluster

	// Map of Envoy instances connected to this pilot
	xdsNodes map[string]*model.Proxy
}

// XDSNode represents information associated with a single Envoy sidecar
type XDSNode struct{}

// Hash returns a globally unique ID for a given Envoy node
// This implements the interface needed by xds cache
func (e XDSNode) Hash(node *xdsapi.Node) (xdscache.Key, error) {
	// Use the envoy node ID (e.g., sidecar~a.b.c.d~...) as the hash key
	return xdscache.Key(node.Id), nil
}

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	model.Environment
	webhookClient   *http.Client
	webhookEndpoint string
	indices         *Indices
	// protos per envoy
	xdsConfigCache *xdscache.Cache
	version        int // This is the version info we send to envoy per xds
}

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	Port            int
	MonitoringPort  int
	EnableProfiling bool
	WebhookEndpoint string
}

// Start XDS servers on a given port
// xdscache.Cache is a simple cache that holds the entire config for each envoy connected to pilot
func startXDS(ctx context.Context, xdsConfigCache *xdscache.Cache, port int) {
	server := xdsserver.NewServer(*xdsConfigCache)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Errorf("failed to listen %v", err)
	}

	xdsapi.RegisterClusterDiscoveryServiceServer(grpcServer, server)
	xdsapi.RegisterEndpointDiscoveryServiceServer(grpcServer, server)
	xdsapi.RegisterListenerDiscoveryServiceServer(grpcServer, server)
	xdsapi.RegisterRouteDiscoveryServiceServer(grpcServer, server)
	log.Infof("XDS servers listening on %d", port)

	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			log.Errorf("Failed to start ADS gRPC server: %v", err)
		}
	}()
	<-ctx.Done()
	grpcServer.GracefulStop()
}

// RegisterNewEnvoy is a callback invoked by the xds server, when
// creating a new config snapshot set (LDS, RDS, CDS, EDS) for an
// Envoy node
func (ds *DiscoveryService) RegisterNewEnvoy(envoyNodeId xdscache.Key) {
	indices := ds.indices
	indices.indicesMutex.Lock()
	defer indices.indicesMutex.Unlock()
	serviceNode := string(envoyNodeId)
	proxy, err := model.ParseServiceNode(serviceNode)
	if err != nil {
		log.Errorf("Failed to parse service node: %v - %v", serviceNode, err)
		return
	}

	indices.xdsNodes[serviceNode] = &proxy
	var temp []*model.Proxy
	temp = append(temp, &proxy)
	ds.UpdateEnvoy(temp)
}

// UpdateEnvoy pushes configuration to the specified list of Envoys
func (ds *DiscoveryService) UpdateEnvoy(serviceNodes []*model.Proxy) {
	indices := ds.indices
	indices.indicesMutex.RLock()
	defer indices.indicesMutex.RUnlock()

	for _, proxy := range serviceNodes {
		clusterLoadAssignments := make([]proto.Message, len(indices.envoyClusters))
		clusters := make([]proto.Message, len(indices.envoyClusters))
		routes := make([]proto.Message, len(indices.routeConfigs))
		listeners := make([]proto.Message, len(indices.listeners))

		// The code below assumes that everyone has same set of clusters, listeners, etc.
		// This would have to be tweaked to populate cluster array based on source match rules in route rules
		// Even though we are copying the two arrays, its essentially just a copy of pointer array and not
		// the entire proto. We still maintain the proto code. proto.Message is just an interface
		for _, cval := range indices.envoyClusters {
			clusterLoadAssignments = append(clusterLoadAssignments, proto.Message(cval.protoEDSresp))
			clusters = append(clusters, proto.Message(cval.protoEnvoyCluster))
		}

		for _, r := range indices.routeConfigs {
			routes = append(routes, proto.Message(r.protoRouteConfiguration))
		}

		for _, l := range indices.listeners {
			listeners = append(listeners, proto.Message(l))
		}

		snapshot := xdscache.NewSnapshot(fmt.Sprintf("%s-version-%d", proxy.ID, ds.version),
			clusterLoadAssignments, clusters, routes, listeners)
		(*ds.xdsConfigCache).SetSnapshot(xdscache.Key(proxy.ID), snapshot)
	}
}

// UpdateIndices updates the indices in Pilot in response to a change in service registry, or route rules,
// and pushes out updates to all the envoys connected to this pilot. Once we add/remove/update clusters,
// listeners, etc., we call the SetSnapshot function of the envoyConfigCache. Changing the cache snapshot
// for a node will cause the server to push the updated listeners, clusters, etc. to the respective envoy.
// The xds server implementation should take care of pushing the resources in the correct order (i.e. clusters,
// then endpoints, then listeners
func (ds *DiscoveryService) UpdateIndices() {
	// TODO: We need to batch calls to UpdateIndices
	// and collapse them such that we update things every second at minimum.
	services, err := ds.Services()
	if err != nil {
		log.Errorf("Failed to obtain all services: %v", err)
		return
	}

	// ensure services are ordered to simplify generation logic
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })

	indices := ds.indices
	indices.indicesMutex.Lock()
	defer indices.indicesMutex.Unlock()
	for _, proxy := range indices.xdsNodes {
		proxyInstances, err := ds.GetProxyServiceInstances(*proxy)
		if err != nil {
			log.Errorf("Unable to retrieve proxy instances for %s: %v", proxy.ID, err)
			continue
		}
		listeners, clusters := buildOutboundListeners(ds.Environment.Mesh, proxy, proxyInstances,
			services, ds.IstioConfigStore)
	}

	// Increment the global version number served from this pilot
	ds.version = ds.version + 1
}

// NewDiscoveryService creates an Envoy discovery service on a given port
func NewDiscoveryService(ctx context.Context, ctl model.Controller, configCache model.ConfigStoreCache,
	environment model.Environment, o DiscoveryServiceOptions) (*DiscoveryService, error) {
	ds := &DiscoveryService{
		Environment: environment,
		indices: &Indices{
			listeners:     make(map[int]*xdsapi.Listener),
			virtualHosts:  make(map[string]*VirtualHost),
			routeConfigs:  make(map[string]*RouteConfiguration),
			envoyClusters: make(map[string]*EnvoyCluster),
			endpoints:     make(map[string]*EnvoyEndpoint),
		},
		version: 0,
	}

	xdsConfigCache := xdscache.NewSimpleCache(XDSNode{}, ds.RegisterNewEnvoy)
	ds.xdsConfigCache = &xdsConfigCache

	// TODO: this has to change to a GRPC webhook
	ds.webhookEndpoint, ds.webhookClient = util.NewWebHookClient(o.WebhookEndpoint)

	// set up the rest/grpc server
	startXDS(ctx, ds.xdsConfigCache, o.Port)

	// The service model provides two callbacks
	// 1. ServiceHandler callback notifies us of updates to the service object (add/remove/new labels/ports)
	// 2. InstanceHandler notifies us of updates to the instances of a service (add/remove or label changes)
	serviceHandler := func(s *model.Service, e model.Event) { ds.UpdateIndices() }
	instanceHandler := func(i *model.ServiceInstance, e model.Event) { ds.UpdateIndices() }

	if err := ctl.AppendServiceHandler(serviceHandler); err != nil {
		return nil, err
	}
	if err := ctl.AppendInstanceHandler(instanceHandler); err != nil {
		return nil, err
	}

	// Config handler callback is invoked when routing rules change
	configHandler := func(rule model.Config, e model.Event) { ds.UpdateIndices() }
	for _, descriptor := range model.IstioConfigTypes {
		// register a callback for route rules, destination rules, external services, gateways, etc.
		configCache.RegisterEventHandler(descriptor.Type, configHandler)
	}

	return ds, nil
}

func (ds *DiscoveryService) GetAllServicesAndInstances() ([]*model.Service, []*model.ServiceInstance, error) {
	services, err := ds.Services()
	if err != nil {
		log.Errorf("Failed to obtain all services: %v", err)
		return nil, nil, err
	}

	var instances []*model.ServiceInstance
	for _, service := range services {
		if !service.External() {
			for _, port := range service.Ports {
				svcInstances, err := ds.Instances(service.Hostname, []string{port.Name}, nil)
				if err != nil {
					log.Errorf("Failed to obtain instances of service %s: %v", service.Hostname, err)
					return nil, nil, err
				}
				instances = append(instances, svcInstances...)
			}
		}
	}

	return services, instances, nil
}
