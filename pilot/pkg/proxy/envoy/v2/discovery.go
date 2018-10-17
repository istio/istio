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
	"strconv"
	"sync"
	"time"

	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/features/pilot"
)

var (
	// Failsafe to implement periodic refresh, in case events or cache invalidation fail.
	// Disabled by default.
	periodicRefreshDuration = 0 * time.Second

	versionMutex sync.RWMutex

	// version is the timestamp of the last registry event.
	version = "0"

	// versionNum counts versions
	versionNum = 1

	periodicRefreshMetrics = 10 * time.Second

	// clearCacheTime is the max time to squash a series of events.
	// The push will happen 1 sec after the last config change, or after 'clearCacheTime'
	// Default value is 1 second, or the value of PILOT_CACHE_SQUASH env
	clearCacheTime = 1

	// DebounceAfter is the delay added to events to wait
	// after a registry/config event for debouncing.
	// This will delay the push by at least this interval, plus
	// the time getting subsequent events. If no change is
	// detected the push will happen, otherwise we'll keep
	// delaying until things settle.
	DebounceAfter time.Duration

	// DebounceMax is the maximum time to wait for events
	// while debouncing. Defaults to 10 seconds. If events keep
	// showing up with no break for this time, we'll trigger a push.
	DebounceMax time.Duration
)

const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for XDS

	// ClusterType is used for cluster discovery. Typically first request received
	ClusterType = typePrefix + "Cluster"
	// EndpointType is used for EDS and ADS endpoint discovery. Typically second request.
	EndpointType = typePrefix + "ClusterLoadAssignment"
	// ListenerType is sent after clusters and endpoints.
	ListenerType = typePrefix + "Listener"
	// RouteType is sent after listeners.
	RouteType = typePrefix + "RouteConfiguration"
)

func init() {
	cacheSquash := pilot.CacheSquash
	if len(cacheSquash) > 0 {
		t, err := strconv.Atoi(cacheSquash)
		if err == nil {
			clearCacheTime = t
		}
	}

	DebounceAfter = envDuration(pilot.DebounceAfter, 100*time.Millisecond)
	DebounceMax = envDuration(pilot.DebounceMax, 10*time.Second)
}

func envDuration(envVal string, def time.Duration) time.Duration {
	if envVal == "" {
		return def
	}
	d, err := time.ParseDuration(envVal)
	if err != nil {
		adsLog.Warnf("Invalid value %s %v", envVal, err)
		return def
	}
	return d
}

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// Env is the model environment.
	Env *model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *MemServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

	// ConfigController provides readiness info (if initial sync is complete)
	ConfigController model.ConfigStoreCache

	rateLimiter         *rate.Limiter
	concurrentPushLimit chan struct{}

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	DebugConfigs bool

	// mutex protecting global structs updated or read by ADS service, including EDSUpdates and
	// shards.
	mutex sync.RWMutex

	// EndpointShardsByService for a service. This is a global (per-server) list, built from
	// incremental updates.
	EndpointShardsByService map[string]*EndpointShardsByService

	// WorkloadsById keeps track of informations about a workload, based on direct notifications
	// from registry. This acts as a cache and allows detecting changes.
	WorkloadsByID map[string]*Workload

	// edsUpdates keeps track of all service updates since last full push.
	// Key is the hostname (servicename). Value is set when any shard part of the service is
	// updated. This should only be used in the xDS server - will be removed/made private in 1.1,
	// once the last v1 pieces are cleaned. For 1.0.3+ it is used only for tracking incremental
	// pushes between the 2 packages.
	edsUpdates map[string]*EndpointShardsByService

	updateChannel chan *updateReq

	// mutex used for config update scheduling (former cache update mutex)
	updateMutex sync.RWMutex

	// true if a full push is needed after debounce. False if only EDS is required.
	fullPush bool

	// lastPushStart (former lastClearCache) is the time we last started a push
	lastPushStart time.Time

	// lastConfigUpdateTime is the time of the last config event
	lastConfigUpdateTime time.Time

	// confiugUpdateCounter is the counter of 'clearCache' calls
	configUpdateCounter int

	// debouncePushTimerSet is true if a config update was requested and we are
	// waiting for more events, to debounce.
	debouncePushTimerSet bool
}

// updateReq includes info about the requested update.
type updateReq struct {
	full bool
}

// EndpointShardsByService holds the set of endpoint shards of a service. Registries update
// individual shards incrementally. The shards are aggregated and split into
// clusters when a push for the specific cluster is needed.
type EndpointShardsByService struct {

	// Shards is used to track the shards. EDS updates are grouped by shard.
	// Current implementation uses the registry name as key - in multicluster this is the
	// name of the k8s cluster, derived from the config (secret).
	Shards map[string]*EndpointShard

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts map[string]bool
}

// EndpointShard contains all the endpoints for a single shard (subset) of a service.
// Shards are updated atomically by registries. A registry may split a service into
// multiple shards (for example each deployment, or smaller sub-sets).
type EndpointShard struct {
	Shard   string
	Entries []*model.IstioEndpoint
}

// Workload has the minimal info we need to detect if we need to push workloads, and to
// cache data to avoid expensive model allocations.
type Workload struct {
	// Labels
	Labels map[string]string

	// Annotations
	Annotations map[string]string
}

func intEnv(envVal string, def int) int {
	if len(envVal) == 0 {
		return def
	}
	n, err := strconv.Atoi(envVal)
	if err == nil && n > 0 {
		return n
	}
	return def
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, generator core.ConfigGenerator, ctl model.Controller, configCache model.ConfigStoreCache) *DiscoveryServer {
	out := &DiscoveryServer{
		Env:                     env,
		ConfigGenerator:         generator,
		EndpointShardsByService: map[string]*EndpointShardsByService{},
		WorkloadsByID:           map[string]*Workload{},
		edsUpdates:              map[string]*EndpointShardsByService{},
		concurrentPushLimit:     make(chan struct{}, 20), // TODO(hzxuzhonghu): support configuration
		updateChannel:           make(chan *updateReq, 10),
	}
	env.PushContext = model.NewPushContext()
	go out.handleUpdates()

	// Flush cached discovery responses whenever services, service
	// instances, or routing configuration changes.
	serviceHandler := func(*model.Service, model.Event) { out.clearCache() }
	if err := ctl.AppendServiceHandler(serviceHandler); err != nil {
		return nil
	}
	instanceHandler := func(*model.ServiceInstance, model.Event) { out.clearCache() }
	if err := ctl.AppendInstanceHandler(instanceHandler); err != nil {
		return nil
	}

	// Flush cached discovery responses when detecting jwt public key change.
	model.JwtKeyResolver.PushFunc = out.ClearCache

	if configCache != nil {
		// TODO: changes should not trigger a full recompute of LDS/RDS/CDS/EDS
		// (especially mixerclient HTTP and quota)
		configHandler := func(model.Config, model.Event) { out.clearCache() }
		for _, descriptor := range model.IstioConfigTypes {
			configCache.RegisterEventHandler(descriptor.Type, configHandler)
		}
	}

	go out.periodicRefresh()

	go out.periodicRefreshMetrics()

	out.DebugConfigs = pilot.DebugConfigs

	pushThrottle := intEnv(pilot.PushThrottle, 10)
	pushBurst := intEnv(pilot.PushBurst, 100)

	adsLog.Infof("Starting ADS server with rateLimiter=%d burst=%d", pushThrottle, pushBurst)
	out.rateLimiter = rate.NewLimiter(rate.Limit(pushThrottle), pushBurst)

	return out
}

// Register adds the ADS and EDS handles to the grpc server
func (s *DiscoveryServer) Register(rpcs *grpc.Server) {
	// EDS must remain registered for 0.8, for smooth upgrade from 0.7
	// 0.7 proxies will use this service.
	ads.RegisterAggregatedDiscoveryServiceServer(rpcs, s)
}

// Singleton, refresh the cache - may not be needed if events work properly, just a failsafe
// ( will be removed after change detection is implemented, to double check all changes are
// captured)
func (s *DiscoveryServer) periodicRefresh() {
	envOverride := pilot.RefreshDuration
	if len(envOverride) > 0 {
		var err error
		periodicRefreshDuration, err = time.ParseDuration(envOverride)
		if err != nil {
			adsLog.Warn("Invalid value for V2_REFRESH")
		}
	}
	if periodicRefreshDuration == 0 {
		return
	}
	ticker := time.NewTicker(periodicRefreshDuration)
	defer ticker.Stop()
	for range ticker.C {
		adsLog.Infof("ADS: periodic push of envoy configs %s", versionInfo())
		s.AdsPushAll(versionInfo(), s.globalPushContext(), true, nil)
	}
}

// Push metrics are updated periodically (10s default)
func (s *DiscoveryServer) periodicRefreshMetrics() {
	ticker := time.NewTicker(periodicRefreshMetrics)
	defer ticker.Stop()
	for range ticker.C {
		push := s.globalPushContext()
		if push.End != timeZero {
			model.LastPushStatus = push
		}
		push.UpdateMetrics()
		// TODO: env to customize
		//if time.Since(push.Start) > 30*time.Second {
		// Reset the stats, some errors may still be stale.
		//s.env.PushContext = model.NewPushContext()
		//}
	}
}

// Push is called to push changes on config updates using ADS. This is set in DiscoveryService.Push,
// to avoid direct dependencies.
func (s *DiscoveryServer) Push(full bool, edsUpdates map[string]*EndpointShardsByService) {
	if !full {
		adsLog.Infof("XDS Incremental Push EDS:%d", len(edsUpdates))
		go s.AdsPushAll(version, s.globalPushContext(), false, edsUpdates)
		return
	}
	// Reset the status during the push.
	//afterPush := true
	pc := s.globalPushContext()
	if pc != nil {
		pc.OnConfigChange()
	}
	// PushContext is reset after a config change. Previous status is
	// saved.
	t0 := time.Now()
	push := model.NewPushContext()
	push.ServiceAccounts = s.ServiceAccounts
	err := push.InitContext(s.Env)
	if err != nil {
		adsLog.Errorf("XDS: failed to update services %v", err)
		// We can't push if we can't read the data - stick with previous version.
		pushContextErrors.Inc()
		return
	}

	if err = s.ConfigGenerator.BuildSharedPushState(s.Env, push); err != nil {
		adsLog.Errorf("XDS: Failed to rebuild share state in configgen: %v", err)
		totalXDSInternalErrors.Add(1)
		return
	}
	if err := s.updateServiceShards(push); err != nil {
		return
	}

	s.updateMutex.Lock()
	s.Env.PushContext = push
	s.updateMutex.Unlock()

	s.mutex.Lock()
	versionLocal := time.Now().Format(time.RFC3339) + "/" + strconv.Itoa(versionNum)
	versionNum++
	initContextTime := time.Since(t0)
	adsLog.Debugf("InitContext %v for push took %s", versionLocal, initContextTime)
	s.mutex.Unlock()

	// TODO: propagate K8S version and use it instead
	versionMutex.Lock()
	version = versionLocal
	versionMutex.Unlock()

	go s.AdsPushAll(versionLocal, push, true, nil)
}

func nonce() string {
	return uuid.New().String()
}

func versionInfo() string {
	versionMutex.RLock()
	defer versionMutex.RUnlock()
	return version
}

// ServiceAccounts returns the list of service accounts for a service.
// The XDS server incrementally updates the list, by getting the SA from registries.
// Same list is used to compute CDS response.
func (s *DiscoveryServer) ServiceAccounts(serviceName string) []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	sa := []string{}

	// TODO: cache the computed service account map in EndpointShardsByService.

	ep, f := s.EndpointShardsByService[serviceName]
	if !f {
		return sa
	}
	samap := map[string]bool{}
	for _, es := range ep.Shards {
		for _, el := range es.Entries {
			if f := samap[el.ServiceAccount]; !f {
				samap[el.ServiceAccount] = true
			}
		}
	}
	// TODO: we can just return the map.
	for k := range samap {
		sa = append(sa, k)
	}

	return sa
}

// Returns the global push context.
func (s *DiscoveryServer) globalPushContext() *model.PushContext {
	s.updateMutex.RLock()
	defer s.updateMutex.RUnlock()
	return s.Env.PushContext
}

// ClearCache is wrapper for clearCache method, used when new controller gets
// instantiated dynamically
func (s *DiscoveryServer) ClearCache() {
	s.clearCache()
}

// debouncePush is called on config or endpoint changes, to initiate a push.
func (s *DiscoveryServer) debouncePush(startDebounce time.Time) {
	s.updateMutex.RLock()
	since := time.Since(s.lastConfigUpdateTime)
	events := s.configUpdateCounter
	s.updateMutex.RUnlock()

	if since > 2*DebounceAfter ||
		time.Since(startDebounce) > DebounceMax {

		adsLog.Infof("Push debounce stable %d: %v since last change, %v since last push, full=%v",
			events,
			since, time.Since(s.lastPushStart), s.fullPush)

		s.doPush()

	} else {
		time.AfterFunc(DebounceAfter, func() {
			s.debouncePush(startDebounce)
		})
	}
}

// Start the actual push. Called from a timer.
func (s *DiscoveryServer) doPush() {
	// more config update events may happen while doPush is processing.
	// we don't want to lose updates.
	s.updateMutex.Lock()

	s.debouncePushTimerSet = false
	s.lastPushStart = time.Now()
	full := s.fullPush

	s.mutex.Lock()
	// Swap the edsUpdates map - tracking requests for incremental updates.
	// The changes to the map are protected by ds.mutex.
	edsUpdates := s.edsUpdates
	// Reset - any new updates will be tracked by the new map
	s.edsUpdates = map[string]*EndpointShardsByService{}
	s.mutex.Unlock()

	// Update the config values, next ConfigUpdate and eds updates will use this
	s.fullPush = false

	s.updateMutex.Unlock()

	s.Push(full, edsUpdates)
}

// clearCache will clear all envoy caches. Called by service, instance and config handlers.
// This will impact the performance, since envoy will need to recalculate.
func (s *DiscoveryServer) clearCache() {
	s.ConfigUpdate(true)
}

// ConfigUpdate implements ConfigUpdater interface, used to request pushes.
// It replaces the 'clear cache' from v1.
func (s *DiscoveryServer) ConfigUpdate(full bool) {
	s.updateChannel <- &updateReq{full: full}
}

// Debouncing and update request happens in a separate thread, it uses locks
// and we want to avoid complications, ConfigUpdate may already hold other locks.
func (s *DiscoveryServer) handleUpdates() {
	for {
		select {
		case r, _ := <-s.updateChannel:

			if DebounceAfter == 0 {
				go s.doPush()
				continue
			}
			s.updateMutex.Lock()

			if r.full {
				s.fullPush = true
			}
			s.configUpdateCounter++

			s.lastConfigUpdateTime = time.Now()

			if !s.debouncePushTimerSet {
				s.debouncePushTimerSet = true
				startDebounce := s.lastConfigUpdateTime
				time.AfterFunc(DebounceAfter, func() {
					s.debouncePush(startDebounce)
				})
			} // else: debounce in progress - it'll keep delaying the push

			s.updateMutex.Unlock()
		}
	}
}
