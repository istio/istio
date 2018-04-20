//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package local

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/plugin/registry"
	envoyv1 "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/log"
)

const (
	defaultGrpcMaxStreams = 100000
)

type args struct {
	Namespace               string
	Mesh                    *meshconfig.MeshConfig
	ServiceControllers      *aggregate.Controller
	ConfigDir               string
	MixerServiceAccountName []string
	DiscoveryServiceOptions envoyv1.DiscoveryServiceOptions
}

// Pilot is an abstraction that manages the bootstrapping of an in-memory Pilot discovery service.
type Pilot struct {
	discoveryOptions envoyv1.DiscoveryServiceOptions
	configController model.ConfigStoreCache
	configMonitor    *configmonitor.Monitor
	discoveryService *envoyv1.DiscoveryService
	xdsServer        *envoyv2.DiscoveryServer
	grpcServer       *grpc.Server
	httpServer       *http.Server
}

// NewPilot creates a new Pilot instance with the given args.
func NewPilot(args args) (*Pilot, error) {
	// TODO(nmittler): Better place for this?
	envoyv1.ValidateClusters = false

	configController := configController()
	configMonitor := fileMonitor(args.ConfigDir, configController)

	environment := model.Environment{
		Mesh:             args.Mesh,
		IstioConfigStore: model.MakeIstioStore(configController),
		ServiceDiscovery: args.ServiceControllers,
		ServiceAccounts:  args.ServiceControllers,
		MixerSAN:         args.MixerServiceAccountName,
	}

	// Set up discovery service
	discovery, err := envoyv1.NewDiscoveryService(
		args.ServiceControllers,
		configController,
		environment,
		args.DiscoveryServiceOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery service: %v", err)
	}

	// For now we create the gRPC server sourcing data from Pilot's older data model.
	grpcServer := grpcServer()
	xdsServer := envoyv2.NewDiscoveryServer(grpcServer, environment, v1alpha3.NewConfigGenerator(registry.NewPlugins()))
	xdsServer.InitDebug(discovery.RestContainer.ServeMux, args.ServiceControllers)

	// TODO: decouple v2 from the cache invalidation, use direct listeners.
	envoyv1.V2ClearCache = xdsServer.ClearCacheFunc()

	httpServer := &http.Server{
		Addr:    ":" + strconv.Itoa(args.DiscoveryServiceOptions.Port),
		Handler: discovery.RestContainer,
	}

	// TODO(nmittler): Add monitoring port

	return &Pilot{
		discoveryOptions: args.DiscoveryServiceOptions,
		configController: configController,
		configMonitor:    configMonitor,
		discoveryService: discovery,
		xdsServer:        xdsServer,
		grpcServer:       grpcServer,
		httpServer:       httpServer,
	}, nil
}

// Start starts the Pilot discovery service.
func (p *Pilot) Start(stop chan struct{}) error {
	// Start the HTTP server.
	addr := p.httpServer.Addr
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	// Start the GRPC server.
	grpcListener, err := net.Listen("tcp", p.discoveryOptions.GrpcAddr)
	if err != nil {
		return err
	}

	// Now run all the servers.
	go p.configController.Run(stop)
	p.configMonitor.Start(stop)
	go func() {
		if err = p.httpServer.Serve(listener); err != nil {
			log.Warna(err)
		}
	}()
	go func() {
		if err = p.xdsServer.GrpcServer.Serve(grpcListener); err != nil {
			log.Warna(err)
		}
	}()

	go func() {
		<-stop
		err = p.httpServer.Close()
		if err != nil {
			log.Warna(err)
		}
		p.xdsServer.GrpcServer.Stop()
	}()
	return nil
}

func configController() model.ConfigStoreCache {
	store := memory.Make(bootstrap.ConfigDescriptor)
	return memory.NewController(store)
}

func fileMonitor(fileDir string, configController model.ConfigStore) *configmonitor.Monitor {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, bootstrap.ConfigDescriptor)
	return configmonitor.NewMonitor(configController, bootstrap.FilepathWalkInterval, fileSnapshot.ReadConfigFiles)
}

func grpcServer() *grpc.Server {
	// TODO for now use hard coded / default gRPC options. The constructor may evolve to use interfaces that guide specific options later.
	// Example:
	//		grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(someconfig.MaxConcurrentStreams)))
	var grpcOptions []grpc.ServerOption

	var interceptors []grpc.UnaryServerInterceptor

	// TODO: log request interceptor if debug enabled.

	// setup server prometheus monitoring (as final interceptor in chain)
	interceptors = append(interceptors, prometheus.UnaryServerInterceptor)
	prometheus.EnableHandlingTimeHistogram()

	grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)))

	// Temp setting, default should be enough for most supported environments. Can be used for testing
	// envoy with lower values.
	maxStreams := defaultGrpcMaxStreams
	maxStreamsEnv := os.Getenv("ISTIO_GPRC_MAXSTREAMS")
	if len(maxStreamsEnv) > 0 {
		maxStreams, _ = strconv.Atoi(maxStreamsEnv)
	}
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(maxStreams)))

	// get the grpc server wired up
	grpc.EnableTracing = true

	return grpc.NewServer(grpcOptions...)
}
