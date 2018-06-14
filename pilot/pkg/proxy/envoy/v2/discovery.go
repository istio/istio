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
	periodicRefreshDuration = 60 * time.Second

	versionMutex sync.Mutex
	// version is update by registry events.
	version = time.Now()
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
	env model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *MemServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

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
func NewDiscoveryServer(env model.Environment, generator core.ConfigGenerator) *DiscoveryServer {
	out := &DiscoveryServer{
		env:             env,
		ConfigGenerator: generator,
	}

	envOverride := os.Getenv("V2_REFRESH")
	if len(envOverride) > 0 {
		var err error
		periodicRefreshDuration, err = time.ParseDuration(envOverride)
		if err != nil {
			adsLog.Warn("Invalid value for V2_REFRESH")
			periodicRefreshDuration = 0 // this is also he default, but setting it explicitly
		}
	}
	if periodicRefreshDuration > 0 {
		go periodicRefresh()
	}

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
func periodicRefresh() {
	ticker := time.NewTicker(periodicRefreshDuration)
	defer ticker.Stop()
	for range ticker.C {
		PushAll()
	}
}

// PushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func PushAll() {
	versionMutex.Lock()
	version = time.Now()
	versionMutex.Unlock()

	adsPushAll()
}

// ClearCacheFunc returns a function that invalidates v2 caches and triggers a push.
// This is used for transition, once the new config model is in place we'll have separate
// functions for each event and push only configs that need to be pushed.
// This is currently called from v1 and has attenuation/throttling.
func (s *DiscoveryServer) ClearCacheFunc() func() {
	return func() {
		s.updateModel()

		s.modelMutex.RLock()
		adsLog.Infof("XDS: Registry event, pushing. Services: %d, "+
			"VirtualServices: %d, ConnectedEndpoints: %d", len(s.services), len(s.virtualServices), edsClientCount())
		monServices.Set(float64(len(s.services)))
		monVServices.Set(float64(len(s.virtualServices)))
		s.modelMutex.RUnlock()

		PushAll()
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
	return version.String()
}
