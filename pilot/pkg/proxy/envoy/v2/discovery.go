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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pkg/log"
)

var (
	// Failsafe to implement periodic refresh, in case events or cache invalidation fail.
	// TODO: remove after events get enough testing
	periodicRefreshDuration = os.Getenv("V2_REFRESH")
	responseTickDuration    = time.Second * 15

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
	RouteType = typePrefix + "Route"
)

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// GrpcServer supports gRPC for xDS v2 services.
	GrpcServer *grpc.Server
	// env is the model environment.
	env model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *MemServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator *v1alpha3.ConfigGeneratorImpl
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(grpcServer *grpc.Server, env model.Environment, generator *v1alpha3.ConfigGeneratorImpl) *DiscoveryServer {
	out := &DiscoveryServer{
		GrpcServer:      grpcServer,
		env:             env,
		ConfigGenerator: generator,
	}

	// EDS must remain registered for 0.8, for smooth upgrade from 0.7
	// 0.7 proxies will use this service.
	xdsapi.RegisterEndpointDiscoveryServiceServer(out.GrpcServer, out)
	ads.RegisterAggregatedDiscoveryServiceServer(out.GrpcServer, out)

	if len(periodicRefreshDuration) > 0 {
		periodicRefresh()
	}

	return out
}

// Singleton, refresh the cache - may not be needed if events work properly, just a failsafe
// ( will be removed after change detection is implemented, to double check all changes are
// captured)
func periodicRefresh() {
	var err error
	responseTickDuration, err = time.ParseDuration(periodicRefreshDuration)
	if err != nil {
		return
	}
	ticker := time.NewTicker(responseTickDuration)
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

	log.Infoa("XDS: Registry event - pushing all configs")

	adsPushAll()
}

func nonce() string {
	return time.Now().String()
}

func versionInfo() string {
	versionMutex.Lock()
	defer versionMutex.Unlock()
	return version.String()
}
