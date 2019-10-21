// Copyright 2019 Istio Authors
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

package istiod

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/filewatcher"

	meshconfig "istio.io/api/mesh/v1alpha1"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

// Server contains the runtime configuration for Istiod.
type Server struct {
	HTTPListeningAddr       net.Addr
	GRPCListeningAddr       net.Addr
	SecureGRPCListeningAddr net.Addr
	MonitorListeningAddr    net.Addr

	EnvoyXdsServer    *envoyv2.DiscoveryServer
	ServiceController *aggregate.Controller

	Mesh         *meshconfig.MeshConfig
	MeshNetworks *meshconfig.MeshNetworks

	ConfigStores []model.ConfigStoreCache

	// Underlying config stores. To simplify, this is a configaggregate instance, created just before
	// start from the configStores
	ConfigController model.ConfigStoreCache

	// Interface abstracting all config operations, including the high-level objects
	// and the low-level untyped model.ConfigStore
	IstioConfigStore model.IstioConfigStore

	startFuncs       []startFunc
	httpServer       *http.Server
	GrpcServer       *grpc.Server
	SecureHTTPServer *http.Server
	SecureGRPCServer *grpc.Server

	mux         *http.ServeMux
	fileWatcher filewatcher.FileWatcher
	Args        *PilotArgs

	CertKey      []byte
	CertChain    []byte
	RootCA       []byte
	Galley       *GalleyServer
	grpcListener net.Listener
	httpListener net.Listener
	Environment  *model.Environment

	// basePort defaults to 15000, used to allow multiple control plane instances on same machine
	// for testing.
	basePort           int32
	secureGrpcListener net.Listener
}

// InitCommon starts the common services - metrics. Ctrlz is currently started by Galley, will need
// to be refactored and moved here.
func (s *Server) InitCommon(args *PilotArgs) {

	_, addr, err := startMonitor(args.DiscoveryOptions.MonitoringAddr, s.mux)
	if err != nil {
		return
	}
	s.MonitorListeningAddr = addr
}

// NewIstiod creates the common server and loads the default config.
//
// A minimal set of Istio Env variables are also used.
// This is expected to run in a Docker or K8S environment, with a volume with user configs mounted.
//
// Defaults:
// - http port 15007
// - grpc on 15010
//- config from $ISTIO_CONFIG or ./conf
func NewIstiod(confDir string) (*Server, error) {
	baseDir := "." // TODO: env ISTIO_HOME or HOME ?

	// TODO: 15006 can't be configured currently
	// TODO: 15090 (prometheus) can't be configured. It's in the bootstrap file, so easy to replace

	meshCfgFile := baseDir + confDir + "/mesh"

	// Create a test pilot discovery service configured to watch the tempDir.
	args := &PilotArgs{
		DomainSuffix: "cluster.local",

		Mesh: MeshArgs{
			ConfigFile:      meshCfgFile,
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: ConfigArgs{},

		Plugins: DefaultPlugins, // TODO: Should it be in MeshConfig ? Env override until it's done.

		// MCP is messing up with the grpc settings...
		MCPMaxMessageSize:        1024 * 1024 * 64,
		MCPInitialWindowSize:     1024 * 1024 * 64,
		MCPInitialConnWindowSize: 1024 * 1024 * 64,
	}

	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = podNamespaceVar.Get()
	}
	if args.KeepaliveOptions == nil {
		args.KeepaliveOptions = istiokeepalive.DefaultOption()
	}
	if args.Config.ClusterRegistriesNamespace == "" {
		args.Config.ClusterRegistriesNamespace = args.Namespace
	}

	server := &Server{
		Args: args,
	}

	server.fileWatcher = filewatcher.NewWatcher()

	if err := server.WatchMeshConfig(meshCfgFile); err != nil {
		return nil, fmt.Errorf("mesh: %v", err)
	}

	pilotAddress := server.Mesh.DefaultConfig.DiscoveryAddress
	_, port, _ := net.SplitHostPort(pilotAddress)

	// TODO: this was added to allow some config of the base port for VMs to allow multiple instances of istiod,
	// mainly for testing. Probably can be removed - little reason to override the defaults.
	basePortI, _ := strconv.Atoi(port)
	basePortI -= basePortI % 100
	basePort := int32(basePortI)
	server.basePort = basePort

	args.DiscoveryOptions = envoy.DiscoveryServiceOptions{
		HTTPAddr: ":8080", // lots of tools use this
		GrpcAddr: fmt.Sprintf(":%d", basePort+10),
		// Using 12 for K8S-DNS based cert.
		// TODO: We'll also need 11 for Citadel-based cert
		SecureGrpcAddr:  fmt.Sprintf(":%d", basePort+12),
		EnableCaching:   true,
		EnableProfiling: true,
	}
	args.CtrlZOptions = &ctrlz.Options{
		Address: "localhost",
		Port:    uint16(basePort + 13),
	}

	err := server.InitConfig()
	if err != nil {
		return nil, err
	}

	// Galley args
	gargs := settings.DefaultArgs()

	// Default dir.
	// If not set, will attempt to use K8S.
	gargs.ConfigPath = baseDir + "/var/lib/istio/local"
	// TODO: load a json file to override defaults (for all components)

	gargs.ValidationArgs.EnableValidation = true
	gargs.ValidationArgs.EnableReconcileWebhookConfiguration = false
	gargs.APIAddress = fmt.Sprintf("tcp://0.0.0.0:%d", basePort+901)
	gargs.Insecure = true
	gargs.EnableServer = true
	gargs.DisableResourceReadyCheck = true
	// Use Galley Ctrlz for all services.
	gargs.IntrospectionOptions.Port = uint16(basePort + 876)

	// The file is loaded and watched by Galley using galley/pkg/meshconfig watcher/reader
	// Current code in galley doesn't expose it - we'll use 2 Caches instead.

	// Defaults are from pkg/config/mesh

	// Actual files are loaded by galley/pkg/src/fs, which recursively loads .yaml and .yml files
	// The files are suing YAMLToJSON, but interpret Kind, APIVersion

	gargs.MeshConfigFile = meshCfgFile
	gargs.MonitoringPort = uint(basePort + 15)
	// Galley component
	// TODO: runs under same gRPC port.
	server.Galley = NewGalleyServer(gargs)

	// TODO: start injection (only for K8S variant)

	// TODO: start envoy only if TLS certs exist (or bootstrap token and SDS server address is configured)
	//err = startEnvoy(baseDir, &mcfg)
	//if err != nil {
	//	return err
	//}
	return server, nil
}
