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
	"sync/atomic"
	"time"

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
	// The proto representation of this virtual host
	xdsapi.VirtualHost
	// list of route configurations (RDS responses) that hold this virtual host
	// use: When a virtual host changes, we have to update the protos of *all* associated
	// route configurations, and push these to envoys
	routeConfigurations []*RouteConfiguration
	// list of envoy clusters associated with this virtual host.

}

// RouteConfiguration holds the route configuration proto representation and pointers to
// owning listeners
type RouteConfiguration struct {
	// The proto representation of this route configuration
	xdsapi.RouteConfiguration
	// list of virtual hosts attached to this route configuration
	// use: When a route configuration is deleted, we need to update all the
	// constituent virtual hosts - wherein we update the backpointers to *not* point to
	// this route configuration anymore (essentially removing references).
	virtualHosts []*VirtualHost
}

// EnvoyEndpoint holds the endpoint proto representation and pointers to
// owning clusters
type EnvoyEndpoint struct {
	// The proto representation of this endpoint
	xdsapi.LbEndpoint
	// Unique ID associated with this endpoint
	name string
	// list of clusters that contain this endpoint
	// use: when an endpoint is removed or its labels change, we use this
	// slice to update *all* clusters that contain this endpoint.
	clusters []*EnvoyCluster
	// map of labels associated with this endpoint
	labels map[string]string
}

// EnvoyCluster holds the envoy cluster proto representations
type EnvoyCluster struct {
	// The proto representation of this envoy cluster
	xdsapi.Cluster
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

// XDSNodeInfo holds the service instances associated with a given Envoy node,
// and other diagnostic information such as the last version acked by that envoy.
type XDSNodeInfo struct {
	model.Proxy
	instances []*model.ServiceInstance
	// TODO - might need more fine grained option
	version int
}

// Indices is a set of reverse indices to quickly locate endpoints, clusters, routes
// and listeners
type Indices struct {
	// A Mutex to guard the entire Index.
	// TODO: This is super coarse grained. We need more finer grained locks
	// and have to use RCU style semantics. Make updates in a copy of a map
	// and atomically replace the pointer to the said map.
	indicesMutex sync.RWMutex
	rebuildMutex sync.Mutex

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
	// The key is the proxy ID
	xdsNodes map[string]*XDSNodeInfo
}

// XDSNode represents information associated with a single Envoy sidecar
type XDSNode struct{}

// Hash returns a globally unique ID for a given Envoy node
// This implements the interface needed by xds cache
func (e XDSNode) Hash(node *xdsapi.Node) (xdscache.Key, error) {
	// Use the envoy node ID (e.g., sidecar~a.b.c.d~...) as the hash key
	return xdscache.Key(node.Id), nil
}

const (
	// IndexRebuildInterval denotes the interval between
	// two rebuilds of the pilot index. Every "interval" seconds,
	// if the needRebuild flag is set, the index will be rebuilt
	// and updates will be pushed to all Envoys.
	IndexRebuildInterval = time.Second
)

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	model.Environment
	port int
	// Just a collection of interfaces to handle streaming requests
	xdsServer  xdsserver.Server
	grpcServer *grpc.Server

	webhookClient   *http.Client
	webhookEndpoint string
	indices         *Indices
	// protos per envoy
	xdsConfigCache *xdscache.Cache
	// This is the version info we send to envoy per xds
	version int
	// This is the equivalent of a level trigger.
	// The registry callbacks set this to 1. Every N seconds, we
	// check this variable. If set, we clear this value and rebuild indices.
	// We need this coarse grained trigger until we incorporate partial
	// rebuild of indices, which in turn requires precise information about
	// what changed in the service registry. Today, we just have two coarse
	// grained handlers (appendService, appendInstance) that do not tell us
	// what piece of information changed.
	needRebuild uint64
}

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	Port            int
	MonitoringPort  int
	EnableProfiling bool
	WebhookEndpoint string
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
		port:    o.Port,
	}

	xdsConfigCache := xdscache.NewSimpleCache(XDSNode{}, ds.registerEnvoy)
	ds.xdsConfigCache = &xdsConfigCache
	ds.xdsServer = xdsserver.NewServer(xdsConfigCache)
	ds.grpcServer = grpc.NewServer()
	xdsapi.RegisterClusterDiscoveryServiceServer(ds.grpcServer, ds.xdsServer)
	xdsapi.RegisterEndpointDiscoveryServiceServer(ds.grpcServer, ds.xdsServer)
	xdsapi.RegisterListenerDiscoveryServiceServer(ds.grpcServer, ds.xdsServer)
	xdsapi.RegisterRouteDiscoveryServiceServer(ds.grpcServer, ds.xdsServer)

	// TODO: this has to change to a GRPC webhook
	ds.webhookEndpoint, ds.webhookClient = util.NewWebHookClient(o.WebhookEndpoint)

	// The service model provides two callbacks
	// 1. ServiceHandler callback notifies us of updates to the service object (add/remove/new labels/ports)
	// 2. InstanceHandler notifies us of updates to the instances of a service (add/remove or label changes)
	serviceHandler := func(s *model.Service, e model.Event) { atomic.StoreUint64(&ds.needRebuild, 1) }
	instanceHandler := func(i *model.ServiceInstance, e model.Event) { atomic.StoreUint64(&ds.needRebuild, 1) }

	if err := ctl.AppendServiceHandler(serviceHandler); err != nil {
		return nil, err
	}
	if err := ctl.AppendInstanceHandler(instanceHandler); err != nil {
		return nil, err
	}

	// Config handler callback is invoked when routing rules change
	configHandler := func(rule model.Config, e model.Event) { atomic.StoreUint64(&ds.needRebuild, 1) }
	for _, descriptor := range model.IstioConfigTypes {
		// register a callback for route rules, destination rules, external services, gateways, etc.
		configCache.RegisterEventHandler(descriptor.Type, configHandler)
	}

	return ds, nil
}

// Start starts the XDS discovery service on the port specified in DiscoveryServiceOptions.
// Serving can be cancelled at any time by closing the provided stop channel.
func (ds *DiscoveryService) Start(stop chan struct{}) (net.Addr, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", ds.port))
	if err != nil {
		log.Errorf("failed to listen %v", err)
		return nil, err
	}

	go func() {
		go func() {
			if err := ds.grpcServer.Serve(listener); err != nil {
				log.Warna(err)
			}
		}()

		// Wait for the stop notification and shutdown the server.
		<-stop
		ds.grpcServer.GracefulStop()
	}()

	log.Infof("XDS server started at %s", listener.Addr().String())

	// Start the index builder
	go func() {
		ticker := time.NewTicker(IndexRebuildInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				doRebuild := atomic.CompareAndSwapUint64(&ds.needRebuild, 1, 0)
				if doRebuild {
					ds.rebuildIndices()
				}
			}
		}
	}()
	return listener.Addr(), nil
}

// RegisterEnvoy is a callback invoked by the xds server, when
// creating a new config snapshot set (LDS, RDS, CDS, EDS) for an
// Envoy node.
func (ds *DiscoveryService) registerEnvoy(envoyNodeId xdscache.Key) {
	indices := ds.indices
	indices.indicesMutex.Lock()
	defer indices.indicesMutex.Unlock()
	serviceNode := string(envoyNodeId)
	proxy, err := model.ParseServiceNode(serviceNode)
	if err != nil {
		log.Errorf("Failed to parse service node: %v - %v", serviceNode, err)
		return
	}

	proxyInstances, err := ds.GetProxyServiceInstances(proxy)
	if err != nil {
		log.Errorf("Unable to retrieve proxy instances for %s: %v", proxy.ID, err)
		return
	}

	// Note: Setting version=0 here handles the case where an envoy disconnects and reconnects.
	// Decoupling XDSNode from Endpoint handles the case where the envoy is not part of the service
	// registry (e.g., pool of LB instances).
	indices.xdsNodes[serviceNode] = &XDSNodeInfo{
		Proxy:     proxy,
		instances: proxyInstances,
		version:   0,
	}

	// Since version is set to 0, we can be sure that this proxy will receive config
	// updates.
	ds.updateEnvoy()
}

// UpdateEnvoy pushes configuration (synthesized from the indices) to envoys
// connected to this instance of Pilot.
// We call the SetSnapshot function of the envoyConfigCache. Changing the cache snapshot
// for a node will cause the server to push the updated listeners, clusters, etc. to the
// respective envoy. The xds server implementation should take care of pushing the resources
// in the correct order, while dealing with nonces/versions.
func (ds *DiscoveryService) updateEnvoy() {
	indices := ds.indices
	indices.indicesMutex.RLock()
	defer indices.indicesMutex.RUnlock()

	for proxyId, xdsInfo := range indices.xdsNodes {
		if xdsInfo.version == ds.version {
			// Skip pushing update if the proxy's config version and our version are same.
			continue
		}
		clusterLoadAssignments := make([]proto.Message, len(indices.envoyClusters))
		clusters := make([]proto.Message, len(indices.envoyClusters))
		routes := make([]proto.Message, len(indices.routeConfigs))
		listeners := make([]proto.Message, len(indices.listeners))

		// The code below assumes that everyone has same set of clusters, listeners, etc.
		// This would have to be tweaked to populate cluster array based on source match rules in route rules
		// Even though we are copying the two arrays, its essentially just a copy of pointer array and not
		// the entire proto. We still maintain the proto code. proto.Message is just an interface
		// PROBLEM: this could have more clusters than what Envoy wants for a given route config
		// when using source based routing
		for _, cval := range indices.envoyClusters {
			clusterLoadAssignments = append(clusterLoadAssignments, proto.Message(cval.protoEDSresp))
			clusters = append(clusters, proto.Message(&cval.Cluster))
		}

		for _, r := range indices.routeConfigs {
			routes = append(routes, proto.Message(&r.RouteConfiguration))
		}

		for _, l := range indices.listeners {
			listeners = append(listeners, proto.Message(l))
		}

		snapshot := xdscache.NewSnapshot(fmt.Sprintf("%s-version-%d", proxyId, ds.version),
			clusterLoadAssignments, clusters, routes, listeners)
		(*ds.xdsConfigCache).SetSnapshot(xdscache.Key(proxyId), snapshot)
	}
}

// TODO: Need a way to detect an envoy being removed and delete its data from pilot indices
// XDS code has to notify us.
func (ds *DiscoveryService) unregisterEnvoy(serviceNodes []*model.Proxy) {

}

// RebuildIndices updates the indices in Pilot in response to a change in service registry, or route rules,
func (ds *DiscoveryService) rebuildIndices() {
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
	// Since we are creating a separate copy of these indices,
	// we only need a read lock, which can also be eliminated if we follow the RCU style
	// updates
	indices.indicesMutex.RLock()

	// TODO: apply this step selectively based on conditions defined above
	// Assumption: All external services are represented in services
	listeners, err := ds.buildOutboundListeners(services)
	if err != nil {
		log.Errorf("Failed to build outbound listeners: %v", err)
		indices.indicesMutex.RUnlock()
		return
	}

	// TODO: apply this step selectively based on conditions defined above
	// Assumption: External services are represented in services. Subsets are
	// represented as virtual services.
	clusters, err := ds.buildOutboundClusters(services)
	if err != nil {
		log.Errorf("Failed to build outbound clusters: %v", err)
		indices.indicesMutex.RUnlock()
		return
	}

	// TODO: apply this step selectively based on conditions defined above
	// Assumption: External services are represented in services. Every route
	// rule can be uniquely associated with atleast one service.
	// TODO: need to handle source specific vhost config
	routeConfigs, err := ds.buildOutboundRouteConfigs(services)
	if err != nil {
		log.Errorf("Failed to build outbound route config: %v", err)
		indices.indicesMutex.RUnlock()
		return
	}

	// TODO: Need source-based routing changes here. This will probably
	// change a ton of stuff above.

	indices.indicesMutex.RUnlock()

	// Now lock all indices for updates
	indices.indicesMutex.Lock()
	indices.listeners = listeners
	indices.envoyClusters = clusters
	indices.routeConfigs = routeConfigs
	// Increment the global version number served from this pilot
	ds.version = ds.version + 1
	indices.indicesMutex.Unlock()

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

func (ds *DiscoveryService) buildOutboundListeners(services []*model.Service) (map[int]*xdsapi.Listener, error) {
	return nil, nil
}

func (ds *DiscoveryService) buildOutboundClusters(services []*model.Service) (map[string]*EnvoyCluster, error) {
	return nil, nil
}

func (ds *DiscoveryService) buildOutboundRouteConfigs(services []*model.Service) (map[string]*RouteConfiguration, error) {
	return nil, nil
}
