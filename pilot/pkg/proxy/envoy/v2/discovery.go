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

	"sync"

	"encoding/json"
	"io/ioutil"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"net/http"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
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
	unknownPeerAddressStr = "Unknown peer address"
)

const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for
	endpointType = typePrefix + "ClusterLoadAssignment"
	clusterType  = typePrefix + "Cluster"
	listenerType = typePrefix + "Listener"
)

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// GrpcServer supports gRPC for xDS v2 services.
	GrpcServer *grpc.Server
	// env is the model environment.
	env model.Environment

	Connections map[string]*EdsConnection

	// memRegistry is used for debug and load testing, allow adding services
	memRegistry *mock.ServiceDiscovery
	memSvcController *memServiceController
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(grpcServer *grpc.Server, env model.Environment) *DiscoveryServer {
	out := &DiscoveryServer{GrpcServer: grpcServer, env: env}
	xdsapi.RegisterEndpointDiscoveryServiceServer(out.GrpcServer, out)
	xdsapi.RegisterListenerDiscoveryServiceServer(out.GrpcServer, out)
	xdsapi.RegisterClusterDiscoveryServiceServer(out.GrpcServer, out)

	if len(periodicRefreshDuration) > 0 {
		periodicRefresh()
	}

	return out
}

type memServiceController struct{
	svcHandlers []func(*model.Service, model.Event)
	instHandlers []func(*model.ServiceInstance, model.Event)
}

func (c *memServiceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.svcHandlers = append(c.svcHandlers, f)
	return nil
}

func (c *memServiceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instHandlers = append(c.instHandlers, f)
	return nil
}

func (c *memServiceController) Run(<-chan struct{}) {}

func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller) {
	// For debugging and load testing v2 we add an memory registry.
	s.memRegistry = mock.NewDiscovery(
		map[string]*model.Service{
			//			mock.HelloService.Hostname: mock.HelloService,
		}, 2)
	s.memSvcController = &memServiceController{}
	registry1 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("memAdapter"),
		ServiceDiscovery: s.memRegistry,
		ServiceAccounts:  s.memRegistry,
		Controller:       s.memSvcController,
	}
	sctl.AddRegistry(registry1)

	mux.HandleFunc("/debug/edsz", EDSz)

	mux.HandleFunc("/debug/cdsz", Cdsz)

	mux.HandleFunc("/debug/ldsz", LDSz)

	mux.HandleFunc("/debug/registryz", s.registryz)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	svcName := req.Form.Get("svc")
	epName := req.Form.Get("ep")
	if svcName != "" {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}
		if epName == "" {
			svc := &model.Service{}
			err = json.Unmarshal(data, svc)
			if err != nil {
				return
			}
			s.memRegistry.AddService(svcName, svc)
		} else {
			svc := &model.ServiceInstance{}
			err = json.Unmarshal(data, svc)
			if err != nil {
				return
			}
			s.memRegistry.AddService(svcName, svc)

		}
	}

	all, err := s.env.ServiceDiscovery.Services()
	if err != nil {
		return
	}
	for _, svc := range all {
		b, err := json.MarshalIndent(svc, "", "  ")
		if err != nil {
			return
		}
		_, _ = w.Write(b)
	}

}

func listServices(w http.ResponseWriter, req *http.Request) {
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

	cdsPushAll()

	// TODO: rename to XdsLegacyPushAll
	edsPushAll() // we want endpoints ready first

	ldsPushAll()
}

func nonce() string {
	return time.Now().String()
}

func versionInfo() string {
	versionMutex.Lock()
	defer versionMutex.Unlock()
	return version.String()
}
