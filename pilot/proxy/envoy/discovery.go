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
	"net"
	"net/http"
	"net/http/pprof"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/emicklei/go-restful"
	"github.com/golang/glog"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
	"istio.io/istio/pilot/tools/version"
)

const (
	metricsPath = "/metrics"
	versionPath = "/version"

	metricsNamespace     = "pilot"
	metricsSubsystem     = "discovery"
	metricLabelCacheName = "cache_name"
	metricLabelMethod    = "method"
	metricBuildVersion   = "build_version"
)

var (
	// Save the build version information.
	buildVersion = version.Line()

	cacheSizeGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "cache_size",
			Help:      "Current size (in bytes) of a single cache within Pilot",
		}, []string{metricLabelCacheName, metricBuildVersion})
	cacheHitCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "cache_hit",
			Help:      "Count of cache hits for a particular cache within Pilot",
		}, []string{metricLabelCacheName, metricBuildVersion})
	cacheMissCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "cache_miss",
			Help:      "Count of cache misses for a particular cache within Pilot",
		}, []string{metricLabelCacheName, metricBuildVersion})
	callCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "calls",
			Help:      "Counter of individual method calls in Pilot",
		}, []string{metricLabelMethod, metricBuildVersion})
	errorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "errors",
			Help:      "Counter of errors encountered during a given method call within Pilot",
		}, []string{metricLabelMethod, metricBuildVersion})

	resourceBuckets = []float64{0, 10, 20, 30, 40, 50, 75, 100, 150, 250, 500, 1000, 10000}
	resourceCounter = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "resources",
			Help:      "Histogram of returned resource counts per method by Pilot",
			Buckets:   resourceBuckets,
		}, []string{metricLabelMethod, metricBuildVersion})
)

func init() {
	prometheus.MustRegister(cacheSizeGauge)
	prometheus.MustRegister(cacheHitCounter)
	prometheus.MustRegister(cacheMissCounter)
	prometheus.MustRegister(callCounter)
	prometheus.MustRegister(errorCounter)
	prometheus.MustRegister(resourceCounter)
}

// DiscoveryService publishes services, clusters, and routes for all proxies
type DiscoveryService struct {
	proxy.Environment
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
	ldsCache *discoveryCache
}

type discoveryCacheStatEntry struct {
	Hit  uint64 `json:"hit"`
	Miss uint64 `json:"miss"`
}

type discoveryCacheStats struct {
	Stats map[string]*discoveryCacheStatEntry `json:"cache_stats"`
}

type discoveryCacheEntry struct {
	data          []byte
	hit           uint64 // atomic
	miss          uint64 // atomic
	resourceCount uint32
}

type discoveryCache struct {
	name     string
	disabled bool
	mu       sync.RWMutex
	cache    map[string]*discoveryCacheEntry
}

func newDiscoveryCache(name string, enabled bool) *discoveryCache {
	return &discoveryCache{
		name:     name,
		disabled: !enabled,
		cache:    make(map[string]*discoveryCacheEntry),
	}
}

func (c *discoveryCache) cachedDiscoveryResponse(key string) ([]byte, uint32, bool) {
	if c.disabled {
		return nil, 0, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Miss - entry.miss is updated in updateCachedDiscoveryResponse
	entry, ok := c.cache[key]
	if !ok || entry.data == nil {
		return nil, 0, false
	}

	// Hit
	atomic.AddUint64(&entry.hit, 1)
	cacheHitCounter.With(c.cacheSizeLabels()).Inc()
	return entry.data, entry.resourceCount, true
}

func (c *discoveryCache) updateCachedDiscoveryResponse(key string, resourceCount uint32, data []byte) {
	if c.disabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.cache[key]
	var cacheSizeDelta float64
	if !ok {
		entry = &discoveryCacheEntry{}
		c.cache[key] = entry
		cacheSizeDelta = float64(len(key) + len(data))
	} else if entry.data != nil {
		cacheSizeDelta = float64(len(data) - len(entry.data))
		glog.Warningf("Overriding cached data for entry %v", key)
	}
	entry.resourceCount = resourceCount
	entry.data = data
	atomic.AddUint64(&entry.miss, 1)
	cacheMissCounter.With(c.cacheSizeLabels()).Inc()
	cacheSizeGauge.With(c.cacheSizeLabels()).Add(cacheSizeDelta)
}

func (c *discoveryCache) clear() {
	// Reset the cache size metric for this cache.
	cacheSizeGauge.Delete(c.cacheSizeLabels())

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
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]*discoveryCacheStatEntry, len(c.cache))
	for k, v := range c.cache {
		stats[k] = &discoveryCacheStatEntry{
			Hit:  atomic.LoadUint64(&v.hit),
			Miss: atomic.LoadUint64(&v.miss),
		}
	}
	return stats
}

func (c *discoveryCache) cacheSizeLabels() prometheus.Labels {
	return prometheus.Labels{
		metricLabelCacheName: c.name,
		metricBuildVersion:   buildVersion,
	}
}

type hosts struct {
	Hosts []*host `json:"hosts"`
}

type host struct {
	Address string `json:"ip_address"`
	Port    int    `json:"port"`
	Tags    *tags  `json:"tags,omitempty"`
}

type tags struct {
	AZ     string `json:"az,omitempty"`
	Canary bool   `json:"canary,omitempty"`

	// Weight is an integer in the range [1, 100] or empty
	Weight int `json:"load_balancing_weight,omitempty"`
}

type ldsResponse struct {
	Listeners Listeners `json:"listeners"`
}

type keyAndService struct {
	Key   string  `json:"service-key"`
	Hosts []*host `json:"hosts"`
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
func NewDiscoveryService(ctl model.Controller, configCache model.ConfigStoreCache,
	environment proxy.Environment, o DiscoveryServiceOptions) (*DiscoveryService, error) {
	out := &DiscoveryService{
		Environment: environment,
		sdsCache:    newDiscoveryCache("sds", o.EnableCaching),
		cdsCache:    newDiscoveryCache("cds", o.EnableCaching),
		rdsCache:    newDiscoveryCache("rds", o.EnableCaching),
		ldsCache:    newDiscoveryCache("lds", o.EnableCaching),
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
	serviceHandler := func(*model.Service, model.Event) { out.clearCache() }
	if err := ctl.AppendServiceHandler(serviceHandler); err != nil {
		return nil, err
	}
	instanceHandler := func(*model.ServiceInstance, model.Event) { out.clearCache() }
	if err := ctl.AppendInstanceHandler(instanceHandler); err != nil {
		return nil, err
	}

	if configCache != nil {
		configHandler := func(model.Config, model.Event) { out.clearCache() }
		configCache.RegisterEventHandler(model.RouteRule.Type, configHandler)
		configCache.RegisterEventHandler(model.IngressRule.Type, configHandler)
		configCache.RegisterEventHandler(model.EgressRule.Type, configHandler)
		configCache.RegisterEventHandler(model.DestinationPolicy.Type, configHandler)

		// TODO: Changes to mixerclient HTTP and Quota should not
		// trigger recompute of full LDS/RDS/CDS/EDS
		configCache.RegisterEventHandler(model.HTTPAPISpec.Type, configHandler)
		configCache.RegisterEventHandler(model.HTTPAPISpecBinding.Type, configHandler)
		configCache.RegisterEventHandler(model.QuotaSpec.Type, configHandler)
		configCache.RegisterEventHandler(model.QuotaSpecBinding.Type, configHandler)
		configCache.RegisterEventHandler(model.EndUserAuthenticationPolicySpec.Type, configHandler)
		configCache.RegisterEventHandler(model.EndUserAuthenticationPolicySpecBinding.Type, configHandler)
	}

	return out, nil
}

// Register adds routes a web service container. This is visible for testing purposes only.
func (ds *DiscoveryService) Register(container *restful.Container) {
	ws := &restful.WebService{}
	ws.Produces(restful.MIME_JSON)

	// List all known services (informational, not invoked by Envoy)
	ws.Route(ws.
		GET("/v1/registration").
		To(ds.ListAllEndpoints).
		Doc("Services in SDS"))

	// This route makes discovery act as an Envoy Service discovery service (SDS).
	// See https://envoyproxy.github.io/envoy/intro/arch_overview/service_discovery.html#service-discovery-service-sds
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/registration/{%s}", ServiceKey)).
		To(ds.ListEndpoints).
		Doc("SDS registration").
		Param(ws.PathParameter(ServiceKey, "tuple of service name and tag name").DataType("string")))

	// This route makes discovery act as an Envoy Cluster discovery service (CDS).
	// See https://envoyproxy.github.io/envoy/configuration/cluster_manager/cds.html#config-cluster-manager-cds
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/clusters/{%s}/{%s}", ServiceCluster, ServiceNode)).
		To(ds.ListClusters).
		Doc("CDS registration").
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	// This route makes discovery act as an Envoy Route discovery service (RDS).
	// See https://lyft.github.io/envoy/docs/configuration/http_conn_man/rds.html
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/routes/{%s}/{%s}/{%s}", RouteConfigName, ServiceCluster, ServiceNode)).
		To(ds.ListRoutes).
		Doc("RDS registration").
		Param(ws.PathParameter(RouteConfigName, "route configuration name").DataType("string")).
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	// This route responds to LDS requests
	// See https://lyft.github.io/envoy/docs/configuration/listeners/lds.html
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/listeners/{%s}/{%s}", ServiceCluster, ServiceNode)).
		To(ds.ListListeners).
		Doc("LDS registration").
		Param(ws.PathParameter(ServiceCluster, "client proxy service cluster").DataType("string")).
		Param(ws.PathParameter(ServiceNode, "client proxy service node").DataType("string")))

	// This route retrieves the Availability Zone of the service node requested
	ws.Route(ws.
		GET(fmt.Sprintf("/v1/az/{%s}/{%s}", ServiceCluster, ServiceNode)).
		To(ds.AvailabilityZone).
		Doc("AZ for service node").
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

	// NOTE: this is a temporary solution to provide bare-bones debug functionality
	// for pilot. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how pilot reports its own metrics.
	container.Handle(metricsPath, promhttp.Handler())
	container.Handle(versionPath, http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Line())); err != nil {
			glog.Errorf("Unable to write version string: %v", err)
		}
	}))
}

// Start starts the Pilot discovery service on the port specified in DiscoveryServiceOptions. If Port == 0, a
// port number is automatically chosen. This method returns the address on which the server is listening for incoming
// connections. Content serving is started by this method, but is executed asynchronously. Serving can be cancelled
// at any time by closing the provided stop channel.
func (ds *DiscoveryService) Start(stop chan struct{}) (net.Addr, error) {
	glog.Infof("Starting discovery service at %v", ds.server.Addr)

	addr := ds.server.Addr
	if addr == "" {
		addr = ":http"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() {
		go func() {
			if err := ds.server.Serve(listener); err != nil {
				glog.Warning(err)
			}
		}()

		// Wait for the stop notification and shutdown the server.
		<-stop
		err := ds.server.Close()
		if err != nil {
			glog.Warning(err)
		}
	}()

	return listener.Addr(), nil
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
	for k, v := range ds.ldsCache.stats() {
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
	ds.ldsCache.resetStats()
}

func (ds *DiscoveryService) clearCache() {
	glog.Infof("Cleared discovery service cache")
	ds.sdsCache.clear()
	ds.cdsCache.clear()
	ds.rdsCache.clear()
	ds.ldsCache.clear()
}

// ListAllEndpoints responds with all Services and is not restricted to a single service-key
func (ds *DiscoveryService) ListAllEndpoints(_ *restful.Request, response *restful.Response) {
	methodName := "ListAllEndpoints"
	incCalls(methodName)

	services := make([]*keyAndService, 0)

	svcs, err := ds.Services()
	if err != nil {
		// If client experiences an error, 503 error will tell envoy to keep its current
		// cache and try again later
		errorResponse(methodName, response, http.StatusServiceUnavailable, "EDS "+err.Error())
		return
	}

	for _, service := range svcs {
		if !service.External() {
			for _, port := range service.Ports {
				hosts := make([]*host, 0)
				instances, err := ds.Instances(service.Hostname, []string{port.Name}, nil)
				if err != nil {
					// If client experiences an error, 503 error will tell envoy to keep its current
					// cache and try again later
					errorResponse(methodName, response, http.StatusInternalServerError, "EDS "+err.Error())
					return
				}
				for _, instance := range instances {
					// Only set tags if theres an AZ to set, ensures nil tags when there isnt
					var t *tags
					if instance.AvailabilityZone != "" {
						t = &tags{AZ: instance.AvailabilityZone}
					}
					hosts = append(hosts, &host{
						Address: instance.Endpoint.Address,
						Port:    instance.Endpoint.Port,
						Tags:    t,
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
		incErrors(methodName)
		glog.Warning(err)
	} else {
		observeResources(methodName, uint32(len(services)))
	}
}

// ListEndpoints responds to EDS requests
func (ds *DiscoveryService) ListEndpoints(request *restful.Request, response *restful.Response) {
	methodName := "ListEndpoints"
	incCalls(methodName)

	key := request.Request.URL.String()
	out, resourceCount, cached := ds.sdsCache.cachedDiscoveryResponse(key)
	if !cached {
		hostname, ports, tags := model.ParseServiceKey(request.PathParameter(ServiceKey))
		// envoy expects an empty array if no hosts are available
		hostArray := make([]*host, 0)
		endpoints, err := ds.Instances(hostname, ports.GetNames(), tags)
		if err != nil {
			// If client experiences an error, 503 error will tell envoy to keep its current
			// cache and try again later
			errorResponse(methodName, response, http.StatusServiceUnavailable, "EDS "+err.Error())
			return
		}
		for _, ep := range endpoints {
			hostArray = append(hostArray, &host{
				Address: ep.Endpoint.Address,
				Port:    ep.Endpoint.Port,
			})
		}
		if out, err = json.MarshalIndent(hosts{Hosts: hostArray}, " ", " "); err != nil {
			errorResponse(methodName, response, http.StatusInternalServerError, "EDS "+err.Error())
			return
		}
		resourceCount = uint32(len(endpoints))
		ds.sdsCache.updateCachedDiscoveryResponse(key, resourceCount, out)
	}
	observeResources(methodName, resourceCount)
	writeResponse(response, out)
}

func (ds *DiscoveryService) parseDiscoveryRequest(request *restful.Request) (proxy.Node, error) {
	node := request.PathParameter(ServiceNode)
	role, err := proxy.ParseServiceNode(node)
	if err != nil {
		return role, multierror.Prefix(err, fmt.Sprintf("unexpected %s: ", ServiceNode))
	}
	return role, nil
}

// AvailabilityZone responds to requests for an AZ for the given cluster node
func (ds *DiscoveryService) AvailabilityZone(request *restful.Request, response *restful.Response) {
	methodName := "AvailabilityZone"
	incCalls(methodName)

	role, err := ds.parseDiscoveryRequest(request)
	if err != nil {
		errorResponse(methodName, response, http.StatusNotFound, "AvailabilityZone "+err.Error())
		return
	}
	instances, err := ds.HostInstances(map[string]bool{role.IPAddress: true})
	if err != nil {
		errorResponse(methodName, response, http.StatusNotFound, "AvailabilityZone "+err.Error())
		return
	}
	if len(instances) <= 0 {
		errorResponse(methodName, response, http.StatusNotFound, "AvailabilityZone couldn't find the given cluster node")
		return
	}
	// All instances are going to have the same IP addr therefore will all be in the same AZ
	writeResponse(response, []byte(instances[0].AvailabilityZone))
}

// ListClusters responds to CDS requests for all outbound clusters
func (ds *DiscoveryService) ListClusters(request *restful.Request, response *restful.Response) {
	methodName := "ListClusters"
	incCalls(methodName)

	key := request.Request.URL.String()
	out, resourceCount, cached := ds.cdsCache.cachedDiscoveryResponse(key)
	if !cached {
		role, err := ds.parseDiscoveryRequest(request)
		if err != nil {
			errorResponse(methodName, response, http.StatusNotFound, "CDS "+err.Error())
			return
		}

		clusters, err := buildClusters(ds.Environment, role)
		if err != nil {
			// If client experiences an error, 503 error will tell envoy to keep its current
			// cache and try again later
			errorResponse(methodName, response, http.StatusServiceUnavailable, "CDS "+err.Error())
			return
		}
		if out, err = json.MarshalIndent(ClusterManager{Clusters: clusters}, " ", " "); err != nil {
			errorResponse(methodName, response, http.StatusInternalServerError, "CDS "+err.Error())
			return
		}
		resourceCount = uint32(len(clusters))
		ds.cdsCache.updateCachedDiscoveryResponse(key, resourceCount, out)
	}
	observeResources(methodName, resourceCount)
	writeResponse(response, out)
}

// ListListeners responds to LDS requests
func (ds *DiscoveryService) ListListeners(request *restful.Request, response *restful.Response) {
	methodName := "ListListeners"
	incCalls(methodName)

	key := request.Request.URL.String()
	out, resourceCount, cached := ds.ldsCache.cachedDiscoveryResponse(key)
	if !cached {
		role, err := ds.parseDiscoveryRequest(request)
		if err != nil {
			errorResponse(methodName, response, http.StatusNotFound, "LDS "+err.Error())
			return
		}

		listeners, err := buildListeners(ds.Environment, role)
		if err != nil {
			// If client experiences an error, 503 error will tell envoy to keep its current
			// cache and try again later
			errorResponse(methodName, response, http.StatusServiceUnavailable, "LDS "+err.Error())
			return
		}
		out, err = json.MarshalIndent(ldsResponse{Listeners: listeners}, " ", " ")
		if err != nil {
			errorResponse(methodName, response, http.StatusInternalServerError, "LDS "+err.Error())
			return
		}
		resourceCount = uint32(len(listeners))
		ds.ldsCache.updateCachedDiscoveryResponse(key, resourceCount, out)
	}
	observeResources(methodName, resourceCount)
	writeResponse(response, out)
}

// ListRoutes responds to RDS requests, used by HTTP routes
// Routes correspond to HTTP routes and use the listener port as the route name
// to identify HTTP filters in the config. Service node value holds the local proxy identity.
func (ds *DiscoveryService) ListRoutes(request *restful.Request, response *restful.Response) {
	methodName := "ListRoutes"
	incCalls(methodName)

	key := request.Request.URL.String()
	out, resourceCount, cached := ds.rdsCache.cachedDiscoveryResponse(key)
	if !cached {
		role, err := ds.parseDiscoveryRequest(request)
		if err != nil {
			errorResponse(methodName, response, http.StatusNotFound, "RDS "+err.Error())
			return
		}

		routeConfigName := request.PathParameter(RouteConfigName)
		routeConfig, err := buildRDSRoute(ds.Mesh, role, routeConfigName,
			ds.ServiceDiscovery, ds.IstioConfigStore)
		if err != nil {
			// If client experiences an error, 503 error will tell envoy to keep its current
			// cache and try again later
			errorResponse(methodName, response, http.StatusServiceUnavailable, "RDS "+err.Error())
			return
		}
		if out, err = json.MarshalIndent(routeConfig, " ", " "); err != nil {
			errorResponse(methodName, response, http.StatusInternalServerError, "RDS "+err.Error())
			return
		}
		resourceCount = uint32(len(routeConfig.VirtualHosts))
		ds.rdsCache.updateCachedDiscoveryResponse(key, resourceCount, out)
	}
	observeResources(methodName, resourceCount)
	writeResponse(response, out)
}

func incCalls(methodName string) {
	callCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Inc()
}

func incErrors(methodName string) {
	errorCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Inc()
}

func observeResources(methodName string, count uint32) {
	resourceCounter.With(prometheus.Labels{
		metricLabelMethod:  methodName,
		metricBuildVersion: buildVersion,
	}).Observe(float64(count))
}

func errorResponse(methodName string, r *restful.Response, status int, msg string) {
	incErrors(methodName)
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
