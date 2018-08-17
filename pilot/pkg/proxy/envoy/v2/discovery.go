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
	"os"
	"strconv"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
)

var (
	// Failsafe to implement periodic refresh, in case events or cache invalidation fail.
	// Disabled by default.
	periodicRefreshDuration = 0 * time.Second

	versionMutex sync.RWMutex

	// version is the timestamp of the last registry event.
	version = "0"

	periodicRefreshMetrics = 10 * time.Second
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

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// env is the model environment.
	env *model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *MemServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

	// ConfigController provides readiness info (if initial sync is complete)
	ConfigController model.ConfigStoreCache

	rateLimiter *rate.Limiter

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	DebugConfigs bool
}

func intEnv(env string, def int) int {
	envValue := os.Getenv(env)
	if len(envValue) == 0 {
		return def
	}
	n, err := strconv.Atoi(envValue)
	if err == nil && n > 0 {
		return n
	}
	return def
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, generator core.ConfigGenerator) *DiscoveryServer {
	out := &DiscoveryServer{
		env:             env,
		ConfigGenerator: generator,
	}
	env.PushContext = model.NewStatus()

	go out.periodicRefresh()

	go out.periodicRefreshMetrics()

	out.DebugConfigs = os.Getenv("PILOT_DEBUG_ADSZ_CONFIG") == "1"

	pushThrottle := intEnv("PILOT_PUSH_THROTTLE", 10)
	pushBurst := intEnv("PILOT_PUSH_BURST", 100)

	adsLog.Infof("Starting ADS server with rateLimiter=%d burst=%d", pushThrottle, pushBurst)
	out.rateLimiter = rate.NewLimiter(rate.Limit(pushThrottle), pushBurst)

	return out
}

// Register adds the ADS and EDS handles to the grpc server
func (s *DiscoveryServer) Register(rpcs *grpc.Server) {
	// EDS must remain registered for 0.8, for smooth upgrade from 0.7
	// 0.7 proxies will use this service.
	xdsapi.RegisterEndpointDiscoveryServiceServer(rpcs, s)
	ads.RegisterAggregatedDiscoveryServiceServer(rpcs, s)
}

// Singleton, refresh the cache - may not be needed if events work properly, just a failsafe
// ( will be removed after change detection is implemented, to double check all changes are
// captured)
func (s *DiscoveryServer) periodicRefresh() {
	envOverride := os.Getenv("V2_REFRESH")
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
		s.AdsPushAll(versionInfo(), s.env.PushContext)
	}
}

// Push metrics are updated periodically (10s default)
func (s *DiscoveryServer) periodicRefreshMetrics() {
	envOverride := os.Getenv("V2_METRICS")
	if len(envOverride) > 0 {
		var err error
		periodicRefreshMetrics, err = time.ParseDuration(envOverride)
		if err != nil {
			adsLog.Warn("Invalid value for V2_METRICS")
		}
	}
	if periodicRefreshMetrics == 0 {
		return
	}

	ticker := time.NewTicker(periodicRefreshMetrics)
	defer ticker.Stop()
	for range ticker.C {
		push := s.env.PushContext
		if push.End != timeZero {
			model.LastPushStatus = push
		}
		push.UpdateMetrics()
		// TODO: env to customize
		//if time.Since(push.Start) > 30*time.Second {
		// Reset the stats, some errors may still be stale.
		//s.env.PushContext = model.NewStatus()
		//}
	}
}

// ClearCacheFunc returns a function that invalidates v2 caches and triggers a push.
// This is used for transition, once the new config model is in place we'll have separate
// functions for each event and push only configs that need to be pushed.
// This is currently called from v1 and has attenuation/throttling.
func (s *DiscoveryServer) ClearCacheFunc() func() {
	return func() {
		// Reset the status during the push.
		//afterPush := true
		if s.env.PushContext != nil {
			s.env.PushContext.OnConfigChange()
		}
		// PushContext is reset after a config change. Previous status is
		// saved.
		t0 := time.Now()
		push := model.NewStatus()
		err := push.InitContext(s.env)
		if err != nil {
			adsLog.Errorf("XDS: failed to update services %v", err)
			// We can't push if we can't read the data - stick with previous version.
			// TODO: metric !!
			// TODO: metric !!
			return
		}
		s.env.PushContext = push
		versionLocal := time.Now().Format(time.RFC3339)
		initContextTime := time.Since(t0)
		adsLog.Debugf("InitContext %v for push takes %s", versionLocal, initContextTime)

		// TODO: propagate K8S version and use it instead
		versionMutex.Lock()
		version = versionLocal
		versionMutex.Unlock()

		go s.AdsPushAll(versionLocal, push)
	}
}

func nonce() string {
	return time.Now().String()
}

func versionInfo() string {
	versionMutex.RLock()
	defer versionMutex.RUnlock()
	return version
}
