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
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
)

var (
	// Failsafe to implement periodic refresh, in case events or cache invalidation fail.
	// Disabled by default.
	periodicRefreshDuration = 0 * time.Second

	versionMutex sync.Mutex

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

	// The next fields are updated by v2 discovery, based on config change events (currently
	// the global invalidation). They are computed once - will not change. The new alpha3
	// API should use this instead of directly accessing ServiceDiscovery or IstioConfigStore.
	modelMutex      sync.RWMutex
	services        []*model.Service
	virtualServices []*networking.VirtualService
	// Temp: the code in alpha3 should use VirtualService directly
	virtualServiceConfigs []model.Config
	//TODO: gateways              []*networking.Gateway
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, generator core.ConfigGenerator) *DiscoveryServer {
	out := &DiscoveryServer{
		env:             env,
		ConfigGenerator: generator,
	}
	env.PushStatus = model.NewStatus()

	go out.periodicRefresh()

	go out.periodicRefreshMetrics()

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
		s.AdsPushAll(versionInfo())
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
		push := s.env.PushStatus
		if push.End != timeZero {
			model.LastPushStatus = push
		}
		push.UpdateMetrics()
		// TODO: env to customize
		if time.Since(push.Start) > 30*time.Second {
			// Reset the stats, some errors may still be stale.
			s.env.PushStatus = model.NewStatus()
		}
	}
}

// ClearCacheFunc returns a function that invalidates v2 caches and triggers a push.
// This is used for transition, once the new config model is in place we'll have separate
// functions for each event and push only configs that need to be pushed.
// This is currently called from v1 and has attenuation/throttling.
func (s *DiscoveryServer) ClearCacheFunc() func() {
	return func() {
		s.updateModel()

		// Reset the status during the push.
		//afterPush := true
		if s.env.PushStatus != nil {
			s.env.PushStatus.OnConfigChange()
		}
		// PushStatus is reset after a config change. Previous status is
		// saved.
		s.env.PushStatus = model.NewStatus()

		// TODO: propagate K8S version and use it instead
		versionMutex.Lock()
		version = time.Now().Format(time.RFC3339)
		versionMutex.Unlock()

		s.AdsPushAll(versionInfo())
	}
}

func (s *DiscoveryServer) updateModel() {
	s.modelMutex.Lock()
	defer s.modelMutex.Unlock()
	services, err := s.env.Services()
	if err != nil {
		adsLog.Errorf("XDS: failed to update services %v", err)
	} else {
		s.services = services
	}
	vservices, err := s.env.List(model.VirtualService.Type, model.NamespaceAll)
	if err != nil {
		adsLog.Errorf("XDS: failed to update virtual services %v", err)
	} else {
		s.virtualServiceConfigs = vservices
		s.virtualServices = make([]*networking.VirtualService, 0, len(vservices))
		for _, ss := range vservices {
			s.virtualServices = append(s.virtualServices, ss.Spec.(*networking.VirtualService))
		}
	}
}

func nonce() string {
	return time.Now().String()
}

func versionInfo() string {
	versionMutex.Lock()
	defer versionMutex.Unlock()
	return version
}
