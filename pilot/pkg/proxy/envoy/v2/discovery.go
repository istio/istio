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
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
)

var (
	// Failsafe to implement periodic refresh, in case events or cache invalidation fail.
	// TODO: remove after events get enough testing
	periodicRefreshDuration = os.Getenv("V2_REFRESH")
	responseTickDuration    = time.Second * 15
)

const (
	unknownPeerAddressStr = "Unknown peer address"
)

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// mesh holds the reference to Pilot's internal data structures that provide mesh discovery capability.
	mesh model.ServiceDiscovery
	// GrpcServer supports gRPC for xDS v2 services.
	GrpcServer *grpc.Server
	// env is the model environment.
	env model.Environment

	Connections map[string]*EdsConnection
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(mesh model.ServiceDiscovery, grpcServer *grpc.Server, env model.Environment) *DiscoveryServer {
	out := &DiscoveryServer{mesh: mesh, GrpcServer: grpcServer, env: env}
	xdsapi.RegisterEndpointDiscoveryServiceServer(out.GrpcServer, out)
	xdsapi.RegisterListenerDiscoveryServiceServer(out.GrpcServer, out)

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
		EdsPushAll()
	}
}
