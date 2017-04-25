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

package envoy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/proxy"
)

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	*proxy.Context
	server *http.Server

	// TODO Profile and optimize cache eviction policy to avoid
	// flushing the entire cache when any route, service, or endpoint
	// changes. An explicit cache expiration policy should be
	// considered with this change to avoid memory exhaustion as the
	// entire cache will no longer be periodically flushed and stale
	// entries can linger in the cache indefinitely.
	sdsCache *discoveryCache
	cdsCache *discoveryCache
	rdsCache *discoveryCache
}

type discoveryCacheStatEntry struct {
	Hit  uint64 `json:"hit"`
	Miss uint64 `json:"miss"`
}

type discoveryCacheStats struct {
	Stats map[string]*discoveryCacheStatEntry `json:"cache_stats"`
}

type discoveryCacheEntry struct {
	data []byte
	hit  uint64 // atomic
	miss uint64 // atomic
}

type discoveryCache struct {
	disabled bool
	mu       sync.RWMutex
	cache    map[string]*discoveryCacheEntry
}

func newDiscoveryCache(enabled bool) *discoveryCache {
	return &discoveryCache{
		disabled: !enabled,
		cache:    make(map[string]*discoveryCacheEntry),
	}
}
func (c *discoveryCache) cachedDiscoveryResponse(key string) ([]byte, bool) {
	if c.disabled {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Miss - entry.miss is updated in updateCachedDiscoveryResponse
	entry, ok := c.cache[key]
	if !ok || entry.data == nil {
		return nil, false
	}

	// Hit
	atomic.AddUint64(&entry.hit, 1)
	return entry.data, true
}

func (c *discoveryCache) updateCachedDiscoveryResponse(key string, data []byte) {
	if c.disabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.cache[key]
	if !ok {
		entry = &discoveryCacheEntry{}
		c.cache[key] = entry
	} else if entry.data != nil {
		glog.Warningf("Overriding cached data for entry %v", key)
	}
	entry.data = data
	atomic.AddUint64(&entry.miss, 1)
}

func (c *discoveryCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range c.cache {
		v.data = nil
	}
}

func (c *discoveryCache) resetStats() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, v := range c.cache {
		atomic.StoreUint64(&v.hit, 0)
		atomic.StoreUint64(&v.miss, 0)
	}
}

func (c *discoveryCache) stats() map[string]*discoveryCacheStatEntry {
	stats := make(map[string]*discoveryCacheStatEntry)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.cache {
		stats[k] = &discoveryCacheStatEntry{
			Hit:  atomic.LoadUint64(&v.hit),
			Miss: atomic.LoadUint64(&v.miss),
		}
	}
	return stats
}

type hosts struct {
	Hosts []*host `json:"hosts"`
}

type host struct {
	Address string `json:"ip_address"`
	Port    int    `json:"port"`

	// Weight is an integer in the range [1, 100] or empty
	Weight int `json:"load_balancing_weight,omitempty"`
}

type keyAndService struct {
	Key   string  `json:"service-key"`
	Hosts []*host `json:"hosts"`
}

type nodeAndCluster struct {
	ServiceCluster string   `json:"service-cluster"`
	ServiceNode    string   `json:"service-node"`
	Clusters       Clusters `json:"clusters"`
}

type routeConfigAndMetadata struct {
	RouteConfigName string         `json:"route-config-name"`
	ServiceCluster  string         `json:"service-cluster"`
	ServiceNode     string         `json:"service-node"`
	VirtualHosts    []*VirtualHost `json:"virtual_hosts"`
}

// Request parameters for discovery services
const (
	ServiceKey      = "service-key"
	ServiceCluster  = "service-cluster"
	ServiceNode     = "service-node"
	RouteConfigName = "route-config-name"
)

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	Port            int
	EnableProfiling bool
	EnableCaching   bool
}

// NewDiscoveryService creates an Envoy discovery service on a given port
func NewDiscoveryService(ctl model.Controller, context *proxy.Context,
	o DiscoveryServiceOptions) (*DiscoveryService, error) {
	out := &DiscoveryService{
		Context:  context,
		sdsCache: newDiscoveryCache(o.EnableCaching),
		cdsCache: newDiscoveryCache(o.EnableCaching),
		rdsCache: newDiscoveryCache(o.EnableCaching),
	}
	container := restful.NewContainer()
	if o.EnableProfiling {
		container.ServeMux.HandleFunc("/debug/pprof/", pprof.Index)
		container.ServeMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		container.ServeMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		container.ServeMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		container.ServeMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	out.Register(container)
	out.server = &http.Server{Addr: ":" + strconv.Itoa(o.Port), Handler: container}

	// Flush cached discovery responses whenever services, service
	// instances, or routing configuration changes.
	serviceHandler := func(s *model.Service, e model.Event) { out.clearCache() }
	if err := ctl.AppendServiceHandler(serviceHandler); err != nil {
		return nil, err
	}
	instanceHandler := func(s *model.ServiceInstance, e model.Event) { out.clearCache() }
	if err := ctl.AppendInstanceHandler(instanceHandler); err != nil {
		return nil, err
	}
	configHandler := func(k model.Key, m proto.Message, e model.Event) { out.clearCache() }
	if err := ctl.AppendConfigHandler(model.RouteRule, configHandler); err != nil {
		return nil, err
	}
	if err := ctl.AppendConfigHandler(model.IngressRule, configHandler); err != nil {
		return nil, err
	}
	if err := ctl.AppendConfigHandler(model.DestinationPolicy, configHandler); err != nil {
		return nil, err
	}

	return out, nil
}

// Register adds routes a web service container
func (ds *DiscoveryService) Register(container *restful.Container) {
	ws := &restful.WebService{}
	ws.Produces(restful.MIME_JSON)

	// List all known services (informational, not invoked by Envoy)
	ws.Route(ws.
		GET("/v1/registration").
		To(ds.ListAllEndpoints).
		Doc("Services in SDS"))

	// This route makes discovery act as an Envoy Service discovery service (SDS).
	// See https://lyft.github.io/envoy/docs/intro/arch_overview/service_discovery.html#arch-overview-service-discovery-sds
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/registration/{%s}", ServiceKey)).
		To(ds.ListEndpoints).
		Doc("SDS registration").
		Param(ws.PathParameter(ServiceKey, "tuple of service name and tag name").DataType("string")))

	// List all known clusters (informational, not invoked by Envoy)
	ws.Route(ws.
		GET("/v1/clusters").
		To(ds.ListAllClusters).
		Doc("Clusters in CDS"))

	// This route makes discovery act as an Envoy Cluster discovery service (CDS).
	// See https://lyft.github.io/envoy/docs/configuration/cluster_manager/cds.html
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/clusters/{%s}/{%s}", ServiceCluster, ServiceNode)).
		To(ds.ListClusters).
		Doc("CDS registration").
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	// List all known routes (informational, not invoked by Envoy)
	ws.Route(ws.
		GET("/v1/routes").
		To(ds.ListAllRoutes).
		Doc("Routes in CDS"))

	// This route makes discovery act as an Envoy Route discovery service (RDS).
	// See https://lyft.github.io/envoy/docs/configuration/http_conn_man/rds.html
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/routes/{%s}/{%s}/{%s}", RouteConfigName, ServiceCluster, ServiceNode)).
		To(ds.ListRoutes).
		Doc("RDS registration").
		Param(ws.PathParameter(RouteConfigName, "route configuration name").DataType("string")).
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	ws.Route(ws.
		GET(fmt.Sprintf("/v1alpha/secret/{%s}/{%s}", ServiceCluster, ServiceNode)).
		To(ds.ListSecret).
		Doc("List TLS secret URI for a listener").
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	ws.Route(ws.
		GET("/cache_stats").
		To(ds.GetCacheStats).
		Doc("Get discovery service cache stats").
		Writes(discoveryCacheStats{}))

	ws.Route(ws.
		POST("/cache_stats_delete").
		To(ds.ClearCacheStats).
		Doc("Clear discovery service cache stats"))

	container.Add(ws)
}

// Run starts the server and blocks
func (ds *DiscoveryService) Run() {
	glog.Infof("Starting discovery service at %v", ds.server.Addr)
	if err := ds.server.ListenAndServe(); err != nil {
		glog.Warning(err)
	}
}

// GetCacheStats returns the statistics for cached discovery responses.
func (ds *DiscoveryService) GetCacheStats(_ *restful.Request, response *restful.Response) {
	stats := make(map[string]*discoveryCacheStatEntry)
	for k, v := range ds.sdsCache.stats() {
		stats[k] = v
	}
	for k, v := range ds.cdsCache.stats() {
		stats[k] = v
	}
	for k, v := range ds.rdsCache.stats() {
		stats[k] = v
	}
	if err := response.WriteEntity(discoveryCacheStats{stats}); err != nil {
		glog.Warning(err)
	}
}

// ClearCacheStats clear the statistics for cached discovery responses.
func (ds *DiscoveryService) ClearCacheStats(_ *restful.Request, _ *restful.Response) {
	ds.sdsCache.resetStats()
	ds.cdsCache.resetStats()
	ds.rdsCache.resetStats()
}

func (ds *DiscoveryService) clearCache() {
	glog.Infof("Cleared discovery service cache")
	ds.sdsCache.clear()
	ds.cdsCache.clear()
	ds.rdsCache.clear()
}

// ListAllEndpoints responds with all Services and is not restricted to a single service-key
func (ds *DiscoveryService) ListAllEndpoints(request *restful.Request, response *restful.Response) {
	services := make([]*keyAndService, 0)
	for _, service := range ds.Discovery.Services() {
		if !service.External() {
			for _, port := range service.Ports {
				hosts := make([]*host, 0)
				for _, instance := range ds.Discovery.Instances(service.Hostname, []string{port.Name}, nil) {
					hosts = append(hosts, &host{
						Address: instance.Endpoint.Address,
						Port:    instance.Endpoint.Port,
					})
				}
				services = append(services, &keyAndService{
					Key:   service.Key(port, nil),
					Hosts: hosts,
				})
			}
		}
	}

	// Sort servicesArray.  This is not strictly necessary, but discovery_test.go will
	// be comparing against a golden example using test/util/diff.go which does a textual comparison
	sort.Slice(services, func(i, j int) bool { return services[i].Key < services[j].Key })

	if err := response.WriteEntity(services); err != nil {
		glog.Warning(err)
	}
}

// ListEndpoints responds to SDS requests
func (ds *DiscoveryService) ListEndpoints(request *restful.Request, response *restful.Response) {
	key := request.Request.URL.String()
	out, cached := ds.sdsCache.cachedDiscoveryResponse(key)
	if !cached {
		hostname, ports, tags := model.ParseServiceKey(request.PathParameter(ServiceKey))
		// envoy expects an empty array if no hosts are available
		hostArray := make([]*host, 0)
		for _, ep := range ds.Discovery.Instances(hostname, ports.GetNames(), tags) {
			hostArray = append(hostArray, &host{
				Address: ep.Endpoint.Address,
				Port:    ep.Endpoint.Port,
			})
		}
		var err error
		if out, err = json.MarshalIndent(hosts{Hosts: hostArray}, " ", " "); err != nil {
			errorResponse(response, http.StatusInternalServerError, err.Error())
			return
		}
		ds.sdsCache.updateCachedDiscoveryResponse(key, out)
	}
	writeResponse(response, out)
}

// ListAllClusters responds to CDS requests that are not limited by a service-cluster and service-node
func (ds *DiscoveryService) ListAllClusters(request *restful.Request, response *restful.Response) {
	allClusters := make([]nodeAndCluster, 0)

	endpoints := ds.allServiceNodes()

	// This sort is not needed, but discovery_test excepts consistent output and sorting achieves it
	sort.Strings(endpoints)

	for _, ip := range endpoints {
		// CDS computes clusters that are referenced by RDS routes for a particular proxy node
		// TODO: this implementation is inefficient as it is recomputing all the routes for all proxies
		// There is a lot of potential to cache and reuse cluster definitions across proxies and also
		// skip computing the actual HTTP routes
		instances := ds.Discovery.HostInstances(map[string]bool{ip: true})
		services := ds.Discovery.Services()
		httpRouteConfigs := buildOutboundHTTPRoutes(instances, services, ds.Accounts, ds.MeshConfig, ds.Config)

		// de-duplicate and canonicalize clusters
		clusters := httpRouteConfigs.clusters().normalize()

		// apply custom policies for HTTP clusters
		for _, cluster := range clusters {
			insertDestinationPolicy(ds.Config, cluster)
		}

		allClusters = append(allClusters, nodeAndCluster{
			ServiceCluster: ds.MeshConfig.IstioServiceCluster,
			ServiceNode:    ip,
			Clusters:       clusters,
		})
	}

	sort.Slice(allClusters, func(i, j int) bool { return allClusters[i].ServiceNode < allClusters[j].ServiceNode })

	if err := response.WriteEntity(allClusters); err != nil {
		glog.Warning(err)
	}
}

// ListClusters responds to CDS requests for all outbound clusters
func (ds *DiscoveryService) ListClusters(request *restful.Request, response *restful.Response) {
	key := request.Request.URL.String()
	out, cached := ds.cdsCache.cachedDiscoveryResponse(key)
	if !cached {
		if sc := request.PathParameter(ServiceCluster); sc != ds.MeshConfig.IstioServiceCluster {
			errorResponse(response, http.StatusNotFound,
				fmt.Sprintf("Unexpected %s %q", ServiceCluster, sc))
			return
		}

		// service-node holds the IP address
		node := request.PathParameter(ServiceNode)

		// CDS computes clusters that are referenced by RDS routes for a particular proxy node
		// TODO: this implementation is inefficient as it is recomputing all the routes for all proxies
		// There is a lot of potential to cache and reuse cluster definitions across proxies and also
		// skip computing the actual HTTP routes
		var httpRouteConfigs HTTPRouteConfigs
		if node == ingressNode {
			httpRouteConfigs, _ = buildIngressRoutes(ds.Config.IngressRules(""))
		} else {
			instances := ds.Discovery.HostInstances(map[string]bool{node: true})
			services := ds.Discovery.Services()
			httpRouteConfigs = buildOutboundHTTPRoutes(instances, services, ds.Accounts, ds.MeshConfig, ds.Config)
		}

		// de-duplicate and canonicalize clusters
		clusters := httpRouteConfigs.clusters().normalize()

		// set connect timeout
		clusters.setTimeout(ds.MeshConfig.ConnectTimeout)

		// apply custom policies for HTTP clusters
		for _, cluster := range clusters {
			insertDestinationPolicy(ds.Config, cluster)
		}

		switch auth := ds.MeshConfig.AuthPolicy; auth {
		case config.ProxyMeshConfig_NONE:
		case config.ProxyMeshConfig_MUTUAL_TLS:
			// apply SSL context to enable mutual TLS between Envoy proxies
			for _, cluster := range clusters {
				ports := model.PortList{cluster.port}.GetNames()
				serviceAccounts := ds.Accounts.GetIstioServiceAccounts(cluster.hostname, ports)
				cluster.SSLContext = buildClusterSSLContext(ds.MeshConfig.AuthCertsPath, serviceAccounts)
			}
		}

		var err error
		if out, err = json.MarshalIndent(ClusterManager{Clusters: clusters}, " ", " "); err != nil {
			errorResponse(response, http.StatusInternalServerError, err.Error())
			return
		}
		ds.cdsCache.updateCachedDiscoveryResponse(key, out)
	}
	writeResponse(response, out)
}

// ListAllRoutes responds to RDS requests that are not limited by a route-config, service-cluster, nor service-node
func (ds *DiscoveryService) ListAllRoutes(request *restful.Request, response *restful.Response) {
	allRoutes := make([]routeConfigAndMetadata, 0)
	endpoints := ds.allServiceNodes()

	for _, ip := range endpoints {
		instances := ds.Discovery.HostInstances(map[string]bool{ip: true})
		services := ds.Discovery.Services()
		httpRouteConfigs := buildOutboundHTTPRoutes(instances, services, ds.Accounts, ds.MeshConfig, ds.Config)

		for port, httpRouteConfig := range httpRouteConfigs {
			allRoutes = append(allRoutes, routeConfigAndMetadata{
				RouteConfigName: strconv.Itoa(port),
				ServiceCluster:  ds.MeshConfig.IstioServiceCluster,
				ServiceNode:     ip,
				VirtualHosts:    httpRouteConfig.VirtualHosts,
			})
		}
	}

	// This sort is not needed, but discovery_test excepts consistent output and sorting achieves it
	// Primary sort key RouteConfigName, secondary ServiceNode, tertiary ServiceCluster
	sort.Slice(allRoutes, func(i, j int) bool {
		if allRoutes[i].RouteConfigName != allRoutes[j].RouteConfigName {
			return allRoutes[i].RouteConfigName < allRoutes[j].RouteConfigName
		} else if allRoutes[i].ServiceNode != allRoutes[j].ServiceNode {
			return allRoutes[i].ServiceNode < allRoutes[j].ServiceNode
		}
		return allRoutes[i].ServiceCluster < allRoutes[j].ServiceCluster
	})

	if err := response.WriteEntity(allRoutes); err != nil {
		glog.Warning(err)
	}
}

// ListRoutes responds to RDS requests, used by HTTP routes
// Routes correspond to HTTP routes and use the listener port as the route name
// to identify HTTP filters in the config. Service node value holds the local proxy identity.
func (ds *DiscoveryService) ListRoutes(request *restful.Request, response *restful.Response) {
	key := request.Request.URL.String()
	out, cached := ds.rdsCache.cachedDiscoveryResponse(key)
	if !cached {
		if sc := request.PathParameter(ServiceCluster); sc != ds.MeshConfig.IstioServiceCluster {
			errorResponse(response, http.StatusNotFound,
				fmt.Sprintf("Unexpected %s %q", ServiceCluster, sc))
			return
		}
		// service-node holds the IP address
		node := request.PathParameter(ServiceNode)

		// route-config-name holds the listener port
		routeConfigName := request.PathParameter(RouteConfigName)
		port, err := strconv.Atoi(routeConfigName)
		if err != nil {
			errorResponse(response, http.StatusNotFound,
				fmt.Sprintf("Unexpected %s %q", RouteConfigName, routeConfigName))
			return
		}

		var httpRouteConfigs HTTPRouteConfigs
		if node == ingressNode {
			httpRouteConfigs, _ = buildIngressRoutes(ds.Config.IngressRules(""))
		} else {
			instances := ds.Discovery.HostInstances(map[string]bool{node: true})
			services := ds.Discovery.Services()
			httpRouteConfigs = buildOutboundHTTPRoutes(instances, services, ds.Accounts, ds.MeshConfig, ds.Config)
		}

		routeConfig, ok := httpRouteConfigs[port]
		if !ok {
			errorResponse(response, http.StatusNotFound,
				fmt.Sprintf("Missing route config for port %d", port))
			return
		}
		if out, err = json.MarshalIndent(routeConfig, " ", " "); err != nil {
			errorResponse(response, http.StatusInternalServerError, err.Error())
			return
		}
		ds.rdsCache.updateCachedDiscoveryResponse(key, out)
	}
	writeResponse(response, out)
}

// ListSecret responds to TLS secret registration
func (ds *DiscoveryService) ListSecret(request *restful.Request, response *restful.Response) {
	// caching is disabled due to lack of secret watch notifications
	if sc := request.PathParameter(ServiceCluster); sc != ds.MeshConfig.IstioServiceCluster {
		errorResponse(response, http.StatusNotFound,
			fmt.Sprintf("Unexpected %s %q", ServiceCluster, sc))
		return
	}

	if sc := request.PathParameter(ServiceNode); sc != ingressNode {
		errorResponse(response, http.StatusNotFound,
			fmt.Sprintf("Unexpected %s %q", ServiceNode, sc))
		return
	}

	_, secret := buildIngressRoutes(ds.Config.IngressRules(""))
	writeResponse(response, []byte(secret))
}

func errorResponse(r *restful.Response, status int, msg string) {
	glog.Warning(msg)
	if err := r.WriteErrorString(status, msg); err != nil {
		glog.Warning(err)
	}
}

func writeResponse(r *restful.Response, data []byte) {
	r.WriteHeader(http.StatusOK)
	if _, err := r.Write(data); err != nil {
		glog.Warning(err)
	}
}

// List all service nodes (typically proxy IPv4 addresses)
func (ds *DiscoveryService) allServiceNodes() []string {
	// Gather service nodes
	endpoints := make(map[string]bool)
	for _, service := range ds.Discovery.Services() {
		if !service.External() {
			// service has Hostname, Address, Ports
			for _, port := range service.Ports {
				for _, instance := range ds.Discovery.Instances(service.Hostname, []string{port.Name}, nil) {
					endpoints[instance.Endpoint.Address] = true
				}
			}
		}
	}

	serviceNodes := make([]string, 0, len(endpoints))
	for ip := range endpoints {
		serviceNodes = append(serviceNodes, ip)
	}

	return serviceNodes
}
