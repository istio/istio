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

package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"

	"istio.io/istio/security/pkg/k8s/chiron"

	"github.com/davecgh/go-spew/spew"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	multierror "github.com/hashicorp/go-multierror"
	prom "github.com/prometheus/client_golang/prometheus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	mcpapi "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	istio_networking_v1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/ledger"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/cmd"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/config/kube/crd/controller"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istio_networking "istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/consul"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	srmemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	configz "istio.io/istio/pkg/mcp/configz/client"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"

	requiredMCPCertCheckFreq = 500 * time.Millisecond

	// DefaultMCPMaxMsgSize is the default maximum message size
	DefaultMCPMaxMsgSize = 1024 * 1024 * 4

	// DefaultMCPInitialWindowSize is the default InitialWindowSize value for the gRPC connection.
	DefaultMCPInitialWindowSize = 1024 * 1024

	// DefaultMCPInitialConnWindowSize is the default Initial ConnWindowSize value for the gRPC connection.
	DefaultMCPInitialConnWindowSize = 1024 * 1024

	// URL types supported by the config store
	// example fs:///tmp/configroot
	fsScheme = "fs"

	// DefaultCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	DefaultCertGracePeriodRatio = 0.5

	// DefaultMinCertGracePeriod is the default minimum grace period for workload cert rotation.
	DefaultMinCertGracePeriod = 10 * time.Minute

	// Default directory to store Pilot key and certificate under $HOME directory
	DefaultDirectoryForKeyCert = "/pilot/key-cert"

	// Default CA certificate path
	// Currently, custom CA path is not supported; no API to get custom CA cert yet.
	DefaultCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

var (
	// FilepathWalkInterval dictates how often the file system is walked for config
	FilepathWalkInterval = 100 * time.Millisecond

	// PilotCertDir is the default location for mTLS certificates used by pilot
	// Visible for tests - at runtime can be set by PILOT_CERT_DIR environment variable.
	PilotCertDir = "/etc/certs/"

	// DefaultPlugins is the default list of plugins to enable, when no plugin(s)
	// is specified through the command line
	DefaultPlugins = []string{
		plugin.Authn,
		plugin.Authz,
		plugin.Health,
		plugin.Mixer,
	}
)

func init() {
	// get the grpc server wired up
	// This should only be set before any RPCs are sent or received by this program.
	grpc.EnableTracing = true

	// Export pilot version as metric for fleet analytics.
	pilotVersion := prom.NewGaugeVec(prom.GaugeOpts{
		Name: "pilot_info",
		Help: "Pilot version and build information.",
	}, []string{"version"})
	prom.MustRegister(pilotVersion)
	pilotVersion.With(prom.Labels{"version": version.Info.String()}).Set(1)
}

// MeshArgs provide configuration options for the mesh. If ConfigFile is provided, an attempt will be made to
// load the mesh from the file. Otherwise, a default mesh will be used with optional overrides.
type MeshArgs struct {
	ConfigFile   string
	MixerAddress string
}

// ConfigArgs provide configuration options for the configuration controller. If FileDir is set, that directory will
// be monitored for CRD yaml files and will update the controller as those files change (This is used for testing
// purposes). Otherwise, a CRD client is created based on the configuration.
type ConfigArgs struct {
	ControllerOptions          controller2.Options
	ClusterRegistriesNamespace string
	KubeConfig                 string
	FileDir                    string

	// DistributionTracking control
	DistributionCacheRetention time.Duration

	DisableInstallCRDs bool

	// DistributionTracking control
	DistributionTrackingEnabled bool
}

func (ca *ConfigArgs) buildLedger() ledger.Ledger {
	var result ledger.Ledger
	if ca.DistributionTrackingEnabled {
		result = ledger.Make(ca.DistributionCacheRetention)
	} else {
		result = &model.DisabledLedger{}
	}
	return result
}

// ConsulArgs provides configuration for the Consul service registry.
type ConsulArgs struct {
	ServerURL string
}

// ServiceArgs provides the composite configuration for all service registries in the system.
type ServiceArgs struct {
	Registries []string
	Consul     ConsulArgs
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	DiscoveryOptions         DiscoveryServiceOptions
	Namespace                string
	Mesh                     MeshArgs
	Config                   ConfigArgs
	Service                  ServiceArgs
	MeshConfig               *meshconfig.MeshConfig
	NetworksConfigFile       string
	CtrlZOptions             *ctrlz.Options
	Plugins                  []string
	MCPMaxMessageSize        int
	MCPInitialWindowSize     int
	MCPInitialConnWindowSize int
	KeepaliveOptions         *istiokeepalive.Options
	// ForceStop is set as true when used for testing to make the server stop quickly
	ForceStop bool
}

// DiscoveryServiceOptions contains options for create a new discovery
// service instance.
type DiscoveryServiceOptions struct {
	// The listening address for HTTP. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPAddr string

	// The listening address for GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	GrpcAddr string

	// The listening address for secure GRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	// "" means disabling secure GRPC, used in test.
	SecureGrpcAddr string

	// The listening address for the monitoring port. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	MonitoringAddr string

	EnableProfiling bool
}

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	HTTPListeningAddr       net.Addr
	GRPCListeningAddr       net.Addr
	SecureGRPCListeningAddr net.Addr
	MonitorListeningAddr    net.Addr

	// TODO(nmittler): Consider alternatives to exposing these directly
	EnvoyXdsServer    *envoyv2.DiscoveryServer
	ServiceController *aggregate.Controller

	mesh             *meshconfig.MeshConfig
	meshNetworks     *meshconfig.MeshNetworks
	configController model.ConfigStoreCache

	kubeClient            kubernetes.Interface
	startFuncs            []startFunc
	multicluster          *clusterregistry.Multicluster
	httpServer            *http.Server
	grpcServer            *grpc.Server
	secureHTTPServer      *http.Server
	secureGRPCServer      *grpc.Server
	istioConfigStore      model.IstioConfigStore
	mux                   *http.ServeMux
	kubeRegistry          *controller2.Controller
	fileWatcher           filewatcher.FileWatcher
	discoveryOptions      *coredatamodel.DiscoveryOptions
	mcpDiscovery          *coredatamodel.MCPDiscovery
	incrementalMcpOptions *coredatamodel.Options
	mcpOptions            *coredatamodel.Options
	certController        *chiron.WebhookController
}

var podNamespaceVar = env.RegisterStringVar("POD_NAMESPACE", "", "")

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args PilotArgs) (*Server, error) {
	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = podNamespaceVar.Get()
	}
	if args.KeepaliveOptions == nil {
		args.KeepaliveOptions = istiokeepalive.DefaultOption()
	}
	if args.Config.ClusterRegistriesNamespace == "" {
		if args.Namespace != "" {
			args.Config.ClusterRegistriesNamespace = args.Namespace
		} else {
			args.Config.ClusterRegistriesNamespace = constants.IstioSystemNamespace
		}
	}

	s := &Server{
		fileWatcher: filewatcher.NewWatcher(),
	}

	prometheus.EnableHandlingTimeHistogram()

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(&args); err != nil {
		return nil, fmt.Errorf("kube client: %v", err)
	}
	if err := s.initMesh(&args); err != nil {
		return nil, fmt.Errorf("mesh: %v", err)
	}
	if err := s.initMeshNetworks(&args); err != nil {
		return nil, fmt.Errorf("mesh networks: %v", err)
	}
	// Certificate controller is created before MCP
	// controller in case MCP server pod waits to mount a certificate
	// to be provisioned by the certificate controller.
	if err := s.initCertController(&args); err != nil {
		return nil, fmt.Errorf("certificate controller: %v", err)
	}
	if err := s.initConfigController(&args); err != nil {
		return nil, fmt.Errorf("config controller: %v", err)
	}
	if err := s.initServiceControllers(&args); err != nil {
		return nil, fmt.Errorf("service controllers: %v", err)
	}
	if err := s.initDiscoveryService(&args); err != nil {
		return nil, fmt.Errorf("discovery service: %v", err)
	}
	if err := s.initMonitor(&args); err != nil {
		return nil, fmt.Errorf("monitor: %v", err)
	}
	if err := s.initClusterRegistries(&args); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}

	if args.CtrlZOptions != nil {
		_, _ = ctrlz.Run(args.CtrlZOptions, nil)
	}

	return s, nil
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServiceOptions.
// If Port == 0, a port number is automatically chosen. Content serving is started by this method,
// but is executed asynchronously. Serving can be canceled at any time by closing the provided stop channel.
func (s *Server) Start(stop <-chan struct{}) error {
	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return err
		}
	}

	return nil
}

// startFunc defines a function that will be used to start one or more components of the Pilot discovery service.
type startFunc func(stop <-chan struct{}) error

// initMonitor initializes the configuration for the pilot monitoring server.
func (s *Server) initMonitor(args *PilotArgs) error { //nolint: unparam
	s.addStartFunc(func(stop <-chan struct{}) error {
		monitor, addr, err := startMonitor(args.DiscoveryOptions.MonitoringAddr, s.mux)
		if err != nil {
			return err
		}
		s.MonitorListeningAddr = addr

		go func() {
			<-stop
			err := monitor.Close()
			log.Debugf("Monitoring server terminated: %v", err)
		}()
		return nil
	})
	return nil
}

// initClusterRegistries starts the secret controller to watch for remote
// clusters and initialize the multicluster structures.
func (s *Server) initClusterRegistries(args *PilotArgs) (err error) {
	if hasKubeRegistry(args) {

		mc, err := clusterregistry.NewMulticluster(s.kubeClient,
			args.Config.ClusterRegistriesNamespace,
			args.Config.ControllerOptions.WatchedNamespace,
			args.Config.ControllerOptions.DomainSuffix,
			args.Config.ControllerOptions.ResyncPeriod,
			s.ServiceController,
			s.EnvoyXdsServer,
			s.meshNetworks)

		if err != nil {
			log.Info("Unable to create new Multicluster object")
			return err
		}

		s.multicluster = mc
	}
	return nil
}

// GetMeshConfig fetches the ProxyMesh configuration from Kubernetes ConfigMap.
func GetMeshConfig(kube kubernetes.Interface, namespace, name string) (*v1.ConfigMap, *meshconfig.MeshConfig, error) {

	if kube == nil {
		defaultMesh := mesh.DefaultMeshConfig()
		return nil, &defaultMesh, nil
	}

	cfg, err := kube.CoreV1().ConfigMaps(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			defaultMesh := mesh.DefaultMeshConfig()
			return nil, &defaultMesh, nil
		}
		return nil, nil, err
	}

	// values in the data are strings, while proto might use a different data type.
	// therefore, we have to get a value by a key
	cfgYaml, exists := cfg.Data[ConfigMapKey]
	if !exists {
		return nil, nil, fmt.Errorf("missing configuration map key %q", ConfigMapKey)
	}

	meshConfig, err := mesh.ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, nil, err
	}
	return cfg, meshConfig, nil
}

// initMesh creates the mesh in the pilotConfig from the input arguments.
func (s *Server) initMesh(args *PilotArgs) error {
	// If a config file was specified, use it.
	if args.MeshConfig != nil {
		s.mesh = args.MeshConfig
		return nil
	}
	var meshConfig *meshconfig.MeshConfig
	var err error

	if args.Mesh.ConfigFile != "" {
		meshConfig, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
		}

		// Watch the config file for changes and reload if it got modified
		s.addFileWatcher(args.Mesh.ConfigFile, func() {
			// Reload the config file
			meshConfig, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
			if err != nil {
				log.Warnf("failed to read mesh configuration, using default: %v", err)
				return
			}
			if !reflect.DeepEqual(meshConfig, s.mesh) {
				log.Infof("mesh configuration updated to: %s", spew.Sdump(meshConfig))
				if !reflect.DeepEqual(meshConfig.ConfigSources, s.mesh.ConfigSources) {
					log.Infof("mesh configuration sources have changed")
					//TODO Need to re-create or reload initConfigController()
				}
				s.mesh = meshConfig
				if s.EnvoyXdsServer != nil {
					s.EnvoyXdsServer.Env.Mesh = meshConfig
					s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
				}
			}
		})
	}

	if meshConfig == nil {
		// Config file either wasn't specified or failed to load - use a default mesh.
		if _, meshConfig, err = GetMeshConfig(s.kubeClient, controller2.IstioNamespace, controller2.IstioConfigMap); err != nil {
			log.Warnf("failed to read the default mesh configuration: %v, from the %s config map in the %s namespace",
				err, controller2.IstioConfigMap, controller2.IstioNamespace)
			return err
		}

		// Allow some overrides for testing purposes.
		if args.Mesh.MixerAddress != "" {
			meshConfig.MixerCheckServer = args.Mesh.MixerAddress
			meshConfig.MixerReportServer = args.Mesh.MixerAddress
		}
	}

	log.Infof("mesh configuration %s", spew.Sdump(meshConfig))
	log.Infof("version %s", version.Info.String())
	log.Infof("flags %s", spew.Sdump(args))

	s.mesh = meshConfig
	return nil
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs) error { //nolint: unparam
	if args.NetworksConfigFile == "" {
		log.Info("mesh networks configuration not provided")
		return nil
	}

	var meshNetworks *meshconfig.MeshNetworks
	var err error

	meshNetworks, err = cmd.ReadMeshNetworksConfig(args.NetworksConfigFile)
	if err != nil {
		log.Warnf("failed to read mesh networks configuration from %q. using default.", args.NetworksConfigFile)
		return nil
	}
	log.Infof("mesh networks configuration %s", spew.Sdump(meshNetworks))
	util.ResolveHostsInNetworksConfig(meshNetworks)
	log.Infof("mesh networks configuration post-resolution %s", spew.Sdump(meshNetworks))
	s.meshNetworks = meshNetworks

	// Watch the networks config file for changes and reload if it got modified
	s.addFileWatcher(args.NetworksConfigFile, func() {
		// Reload the config file
		meshNetworks, err := cmd.ReadMeshNetworksConfig(args.NetworksConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh networks configuration from %q", args.NetworksConfigFile)
			return
		}
		if !reflect.DeepEqual(meshNetworks, s.meshNetworks) {
			log.Infof("mesh networks configuration file updated to: %s", spew.Sdump(meshNetworks))
			util.ResolveHostsInNetworksConfig(meshNetworks)
			log.Infof("mesh networks configuration post-resolution %s", spew.Sdump(meshNetworks))
			s.meshNetworks = meshNetworks
			if s.kubeRegistry != nil {
				s.kubeRegistry.InitNetworkLookup(meshNetworks)
			}
			if s.multicluster != nil {
				s.multicluster.ReloadNetworkLookup(meshNetworks)
			}
			if s.EnvoyXdsServer != nil {
				s.EnvoyXdsServer.Env.MeshNetworks = meshNetworks
				s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
			}
		}
	})

	return nil
}

func (s *Server) getKubeCfgFile(args *PilotArgs) string {
	return args.Config.KubeConfig
}

// initKubeClient creates the k8s client if running in an k8s environment.
func (s *Server) initKubeClient(args *PilotArgs) error {
	if hasKubeRegistry(args) && args.Config.FileDir == "" {
		client, kuberr := kubelib.CreateClientset(s.getKubeCfgFile(args), "")
		if kuberr != nil {
			return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
		}
		s.kubeClient = client

	}

	return nil
}

type mockController struct{}

func (c *mockController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	return nil
}

func (c *mockController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	return nil
}

func (c *mockController) Run(<-chan struct{}) {}

func (s *Server) initMCPConfigController(args *PilotArgs) error {
	ctx, cancel := context.WithCancel(context.Background())
	var clients []*sink.Client
	var conns []*grpc.ClientConn
	var configStores []model.ConfigStoreCache

	reporter := monitoring.NewStatsContext("pilot")

	for _, configSource := range s.mesh.ConfigSources {
		if strings.Contains(configSource.Address, fsScheme+"://") {
			srcAddress, err := url.Parse(configSource.Address)
			if err != nil {
				cancel()
				return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
			}
			if srcAddress.Scheme == fsScheme {
				if srcAddress.Path == "" {
					cancel()
					return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
				}
				store := memory.MakeWithLedger(schemas.Istio, args.Config.buildLedger())
				configController := memory.NewController(store)

				err := s.makeFileMonitor(srcAddress.Path, configController)
				if err != nil {
					cancel()
					return err
				}
				configStores = append(configStores, configController)
				continue
			}
		}

		securityOption, err := mcpSecurityOptions(ctx, cancel, configSource)
		if err != nil {
			return err
		}

		keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    args.KeepaliveOptions.Time,
			Timeout: args.KeepaliveOptions.Timeout,
		})

		initialWindowSizeOption := grpc.WithInitialWindowSize(int32(args.MCPInitialWindowSize))
		initialConnWindowSizeOption := grpc.WithInitialConnWindowSize(int32(args.MCPInitialConnWindowSize))
		msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(args.MCPMaxMessageSize))

		conn, err := grpc.DialContext(
			ctx,
			configSource.Address,
			securityOption,
			msgSizeOption,
			keepaliveOption,
			initialWindowSizeOption,
			initialConnWindowSizeOption)
		if err != nil {
			log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
			cancel()
			return err
		}
		conns = append(conns, conn)
		s.mcpController(args, conn, reporter, &clients, &configStores)

		// create MCP SyntheticServiceEntryController
		if resourceContains(configSource.SubscribedResources, meshconfig.Resource_SERVICE_REGISTRY) {

			//TODO(Nino-K): https://github.com/istio/istio/issues/16976
			args.Service.Registries = []string{string(serviceregistry.MCPRegistry)}
			conn, err := grpc.DialContext(
				ctx,
				configSource.Address,
				securityOption,
				msgSizeOption,
				keepaliveOption,
				initialWindowSizeOption,
				initialConnWindowSizeOption)
			if err != nil {
				log.Errorf("Unable to dial SSE MCP Server %q: %v", configSource.Address, err)
				cancel()
				return err
			}
			conns = append(conns, conn)
			s.sseMCPController(args, conn, reporter, &clients, &configStores)
		}
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		var wg sync.WaitGroup

		for i := range clients {
			client := clients[i]
			wg.Add(1)
			go func() {
				client.Run(ctx)
				wg.Done()
			}()
		}

		go func() {
			<-stop

			// Stop the MCP clients and any pending connection.
			cancel()

			// Close all of the open grpc connections once the mcp
			// client(s) have fully stopped.
			wg.Wait()
			for _, conn := range conns {
				_ = conn.Close() // nolint: errcheck
			}

			_ = reporter.Close()
		}()

		return nil
	})

	// Wrap the config controller with a cache.
	aggregateMcpController, err := configaggregate.MakeCache(configStores)
	if err != nil {
		return err
	}
	s.configController = aggregateMcpController
	return nil
}

func resourceContains(resources []meshconfig.Resource, resource meshconfig.Resource) bool {
	for _, r := range resources {
		if r == resource {
			return true
		}
	}
	return false
}

func mcpSecurityOptions(ctx context.Context, cancel context.CancelFunc, configSource *meshconfig.ConfigSource) (grpc.DialOption, error) {
	securityOption := grpc.WithInsecure()
	if configSource.TlsSettings != nil &&
		configSource.TlsSettings.Mode != istio_networking_v1alpha3.TLSSettings_DISABLE {
		var credentialOption *creds.Options
		switch configSource.TlsSettings.Mode {
		case istio_networking_v1alpha3.TLSSettings_SIMPLE:
		case istio_networking_v1alpha3.TLSSettings_MUTUAL:
			credentialOption = &creds.Options{
				CertificateFile:   configSource.TlsSettings.ClientCertificate,
				KeyFile:           configSource.TlsSettings.PrivateKey,
				CACertificateFile: configSource.TlsSettings.CaCertificates,
			}
		case istio_networking_v1alpha3.TLSSettings_ISTIO_MUTUAL:
			credentialOption = &creds.Options{
				CertificateFile:   path.Join(constants.AuthCertsPath, constants.CertChainFilename),
				KeyFile:           path.Join(constants.AuthCertsPath, constants.KeyFilename),
				CACertificateFile: path.Join(constants.AuthCertsPath, constants.RootCertFilename),
			}
		default:
			log.Errorf("invalid tls setting mode %d", configSource.TlsSettings.Mode)
		}

		if credentialOption == nil {
			transportCreds := creds.CreateForClientSkipVerify()
			securityOption = grpc.WithTransportCredentials(transportCreds)
		} else {
			requiredFiles := []string{
				credentialOption.CACertificateFile,
				credentialOption.KeyFile,
				credentialOption.CertificateFile}
			log.Infof("Secure MCP configured. Waiting for required certificate files to become available: %v",
				requiredFiles)
			for len(requiredFiles) > 0 {
				if _, err := os.Stat(requiredFiles[0]); os.IsNotExist(err) {
					log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredMCPCertCheckFreq)
					select {
					case <-ctx.Done():
						cancel()
						return nil, ctx.Err()
					case <-time.After(requiredMCPCertCheckFreq):
						// retry
					}
					continue
				}
				log.Infof("%v found", requiredFiles[0])
				requiredFiles = requiredFiles[1:]
			}

			watcher, err := creds.WatchFiles(ctx.Done(), credentialOption)
			if err != nil {
				cancel()
				return nil, err
			}
			transportCreds := creds.CreateForClient(configSource.TlsSettings.Sni, watcher)
			securityOption = grpc.WithTransportCredentials(transportCreds)
		}
	}
	return securityOption, nil
}

func (s *Server) mcpController(args *PilotArgs,
	conn *grpc.ClientConn,
	reporter monitoring.Reporter,
	clients *[]*sink.Client,
	configStores *[]model.ConfigStoreCache) {
	clientNodeID := ""
	var collections []sink.CollectionOptions
	for _, c := range schemas.Istio {
		// do not register SSEs for this controller as there is a dedicated controller
		if c.Collection == schemas.SyntheticServiceEntry.Collection {
			continue
		}
		collections = append(collections, sink.CollectionOptions{Name: c.Collection, Incremental: false})
	}
	s.mcpOptions = &coredatamodel.Options{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
		ConfigLedger: args.Config.buildLedger(),
	}

	mcpController := coredatamodel.NewController(s.mcpOptions)
	sinkOptions := &sink.Options{
		CollectionOptions: collections,
		Updater:           mcpController,
		ID:                clientNodeID,
		Reporter:          reporter,
	}

	cl := mcpapi.NewResourceSourceClient(conn)
	mcpClient := sink.NewClient(cl, sinkOptions)
	configz.Register(mcpClient)
	*clients = append(*clients, mcpClient)
	*configStores = append(*configStores, mcpController)
}

func (s *Server) sseMCPController(args *PilotArgs,
	conn *grpc.ClientConn,
	reporter monitoring.Reporter,
	clients *[]*sink.Client,
	configStores *[]model.ConfigStoreCache) {
	clientNodeID := "SSEMCP"
	s.incrementalMcpOptions = &coredatamodel.Options{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
	}
	controller := coredatamodel.NewSyntheticServiceEntryController(s.incrementalMcpOptions)
	s.discoveryOptions = &coredatamodel.DiscoveryOptions{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
	}
	s.mcpDiscovery = coredatamodel.NewMCPDiscovery(controller, s.discoveryOptions)
	incrementalSinkOptions := &sink.Options{
		CollectionOptions: []sink.CollectionOptions{
			{
				Name:        schemas.SyntheticServiceEntry.Collection,
				Incremental: true,
			},
		},
		Updater:  controller,
		ID:       clientNodeID,
		Reporter: reporter,
	}
	incSrcClient := mcpapi.NewResourceSourceClient(conn)
	incMcpClient := sink.NewClient(incSrcClient, incrementalSinkOptions)
	configz.Register(incMcpClient)
	*clients = append(*clients, incMcpClient)
	*configStores = append(*configStores, controller)
}

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
	if len(s.mesh.ConfigSources) > 0 {
		if err := s.initMCPConfigController(args); err != nil {
			return err
		}
	} else if args.Config.FileDir != "" {
		store := memory.Make(schemas.Istio)
		configController := memory.NewController(store)

		err := s.makeFileMonitor(args.Config.FileDir, configController)
		if err != nil {
			return err
		}

		s.configController = configController
	} else {
		cfgController, err := s.makeKubeConfigController(args)
		if err != nil {
			return err
		}

		s.configController = cfgController
	}

	// Defer starting the controller until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	// If running in ingress mode (requires k8s), wrap the config controller.
	if hasKubeRegistry(args) && s.mesh.IngressControllerMode != meshconfig.MeshConfig_OFF {
		// Wrap the config controller with a cache.
		configController, err := configaggregate.MakeCache([]model.ConfigStoreCache{
			s.configController,
			ingress.NewController(s.kubeClient, s.mesh, args.Config.ControllerOptions),
		})
		if err != nil {
			return err
		}

		// Update the config controller
		s.configController = configController

		if ingressSyncer, errSyncer := ingress.NewStatusSyncer(s.mesh, s.kubeClient,
			args.Namespace, args.Config.ControllerOptions); errSyncer != nil {
			log.Warnf("Disabled ingress status syncer due to %v", errSyncer)
		} else {
			s.addStartFunc(func(stop <-chan struct{}) error {
				go ingressSyncer.Run(stop)
				return nil
			})
		}
	}

	// Create the config store.
	s.istioConfigStore = model.MakeIstioStore(s.configController)

	return nil
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	kubeCfgFile := s.getKubeCfgFile(args)
	configClient, err := controller.NewClient(kubeCfgFile, "", schemas.Istio,
		args.Config.ControllerOptions.DomainSuffix, args.Config.buildLedger())
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if !args.Config.DisableInstallCRDs {
		if err = configClient.RegisterResources(); err != nil {
			return nil, multierror.Prefix(err, "failed to register custom resources.")
		}
	}

	return controller.NewController(configClient, args.Config.ControllerOptions), nil
}

func (s *Server) makeFileMonitor(fileDir string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, schemas.Istio)
	fileMonitor := configmonitor.NewMonitor("file-monitor", configController, FilepathWalkInterval, fileSnapshot.ReadConfigFiles)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}

// createK8sServiceControllers creates all the k8s service controllers under this pilot
func (s *Server) createK8sServiceControllers(serviceControllers *aggregate.Controller, args *PilotArgs) (err error) {
	clusterID := string(serviceregistry.KubernetesRegistry)
	log.Infof("Primary Cluster name: %s", clusterID)
	args.Config.ControllerOptions.ClusterID = clusterID
	kubectl := controller2.NewController(s.kubeClient, args.Config.ControllerOptions)
	s.kubeRegistry = kubectl
	serviceControllers.AddRegistry(
		aggregate.Registry{
			Name:             serviceregistry.KubernetesRegistry,
			ClusterID:        clusterID,
			ServiceDiscovery: kubectl,
			Controller:       kubectl,
		})

	return
}

func hasKubeRegistry(args *PilotArgs) bool {
	for _, r := range args.Service.Registries {
		if serviceregistry.ServiceRegistry(r) == serviceregistry.KubernetesRegistry {
			return true
		}
	}
	return false
}

// initServiceControllers creates and initializes the service controllers
func (s *Server) initServiceControllers(args *PilotArgs) error {
	serviceControllers := aggregate.NewController()
	registered := make(map[serviceregistry.ServiceRegistry]bool)
	for _, r := range args.Service.Registries {
		serviceRegistry := serviceregistry.ServiceRegistry(r)
		if _, exists := registered[serviceRegistry]; exists {
			log.Warnf("%s registry specified multiple times.", r)
			continue
		}
		registered[serviceRegistry] = true
		log.Infof("Adding %s registry adapter", serviceRegistry)
		switch serviceRegistry {
		case serviceregistry.MockRegistry:
			s.initMemoryRegistry(serviceControllers)
		case serviceregistry.KubernetesRegistry:
			if err := s.createK8sServiceControllers(serviceControllers, args); err != nil {
				return err
			}
		case serviceregistry.ConsulRegistry:
			if err := s.initConsulRegistry(serviceControllers, args); err != nil {
				return err
			}
		case serviceregistry.MCPRegistry:
			if s.mcpDiscovery != nil {
				serviceControllers.AddRegistry(
					aggregate.Registry{
						Name:             serviceregistry.MCPRegistry,
						ServiceDiscovery: s.mcpDiscovery,
						Controller:       s.mcpDiscovery,
					})
			}
		default:
			return fmt.Errorf("service registry %s is not supported", r)
		}
	}

	serviceEntryStore := external.NewServiceDiscovery(s.configController, s.istioConfigStore)

	// add service entry registry to aggregator by default
	serviceEntryRegistry := aggregate.Registry{
		Name:             "ServiceEntries",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	serviceControllers.AddRegistry(serviceEntryRegistry)

	s.ServiceController = serviceControllers

	// Defer running of the service controllers.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.ServiceController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) initMemoryRegistry(serviceControllers *aggregate.Controller) {
	// MemServiceDiscovery implementation
	discovery1 := srmemory.NewDiscovery(
		map[host.Name]*model.Service{ // srmemory.HelloService.Hostname: srmemory.HelloService,
		}, 2)

	discovery2 := srmemory.NewDiscovery(
		map[host.Name]*model.Service{ // srmemory.WorldService.Hostname: srmemory.WorldService,
		}, 2)

	registry1 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockAdapter1"),
		ClusterID:        "mockAdapter1",
		ServiceDiscovery: discovery1,
		Controller:       &mockController{},
	}

	registry2 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockAdapter2"),
		ClusterID:        "mockAdapter2",
		ServiceDiscovery: discovery2,
		Controller:       &mockController{},
	}
	serviceControllers.AddRegistry(registry1)
	serviceControllers.AddRegistry(registry2)
}

func (s *Server) initDiscoveryService(args *PilotArgs) error {
	environment := &model.Environment{
		Mesh:             s.mesh,
		MeshNetworks:     s.meshNetworks,
		IstioConfigStore: s.istioConfigStore,
		ServiceDiscovery: s.ServiceController,
		PushContext:      model.NewPushContext(),
	}

	s.EnvoyXdsServer = envoyv2.NewDiscoveryServer(environment,
		istio_networking.NewConfigGenerator(args.Plugins))
	s.mux = http.NewServeMux()
	s.EnvoyXdsServer.InitDebug(s.mux, s.ServiceController, args.DiscoveryOptions.EnableProfiling)

	if err := s.initEventHandlers(); err != nil {
		return err
	}

	if s.kubeRegistry != nil {
		// kubeRegistry may use the environment for push status reporting.
		// TODO: maybe all registries should have this as an optional field ?
		s.kubeRegistry.Env = environment
		s.kubeRegistry.InitNetworkLookup(s.meshNetworks)
		s.kubeRegistry.XDSUpdater = s.EnvoyXdsServer
	}

	if s.mcpOptions != nil {
		s.mcpOptions.XDSUpdater = s.EnvoyXdsServer
	}
	if s.incrementalMcpOptions != nil {
		clusterID := args.Config.ControllerOptions.ClusterID
		s.incrementalMcpOptions.XDSUpdater = s.EnvoyXdsServer
		s.incrementalMcpOptions.ClusterID = clusterID
		s.discoveryOptions.Env = environment
		s.discoveryOptions.ClusterID = clusterID
	}

	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc(func(stop <-chan struct{}) error {
		s.EnvoyXdsServer.Start(stop)
		return nil
	})

	// create grpc/http server
	s.initGrpcServer(args.KeepaliveOptions)
	s.httpServer = &http.Server{
		Addr:    args.DiscoveryOptions.HTTPAddr,
		Handler: s.mux,
	}

	// create http listener
	listener, err := net.Listen("tcp", args.DiscoveryOptions.HTTPAddr)
	if err != nil {
		return err
	}
	s.HTTPListeningAddr = listener.Addr()

	// create grpc listener
	grpcListener, err := net.Listen("tcp", args.DiscoveryOptions.GrpcAddr)
	if err != nil {
		return err
	}
	s.GRPCListeningAddr = grpcListener.Addr()

	s.addStartFunc(func(stop <-chan struct{}) error {
		if !s.waitForCacheSync(stop) {
			return fmt.Errorf("failed to sync cache")
		}
		log.Infof("starting discovery service at http=%s grpc=%s", listener.Addr(), grpcListener.Addr())
		go func() {
			if err := s.httpServer.Serve(listener); err != nil {
				log.Warna(err)
			}
		}()
		go func() {
			if err := s.grpcServer.Serve(grpcListener); err != nil {
				log.Warna(err)
			}
		}()

		go func() {
			<-stop
			model.JwtKeyResolver.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := s.httpServer.Shutdown(ctx)
			if err != nil {
				log.Warna(err)
			}
			if args.ForceStop {
				s.grpcServer.Stop()
			} else {
				s.grpcServer.GracefulStop()
			}
		}()

		return nil
	})

	// run secure grpc server
	if args.DiscoveryOptions.SecureGrpcAddr != "" {
		// create secure grpc server
		if err := s.initSecureGrpcServer(args.KeepaliveOptions); err != nil {
			return fmt.Errorf("secure grpc server: %s", err)
		}
		// create secure grpc listener
		secureGrpcListener, err := net.Listen("tcp", args.DiscoveryOptions.SecureGrpcAddr)
		if err != nil {
			return err
		}
		s.SecureGRPCListeningAddr = secureGrpcListener.Addr()

		s.addStartFunc(func(stop <-chan struct{}) error {
			go func() {
				if !s.waitForCacheSync(stop) {
					return
				}

				log.Infof("starting discovery service at secure grpc=%s", secureGrpcListener.Addr())
				go func() {
					// This seems the only way to call setupHTTP2 - it may also be possible to set NextProto
					// on a listener
					err := s.secureHTTPServer.ServeTLS(secureGrpcListener, "", "")
					msg := fmt.Sprintf("Stoppped listening on %s", secureGrpcListener.Addr().String())
					select {
					case <-stop:
						log.Info(msg)
					default:
						panic(fmt.Sprintf("%s due to error: %v", msg, err))
					}
				}()
				go func() {
					<-stop
					if args.ForceStop {
						s.grpcServer.Stop()
					} else {
						s.grpcServer.GracefulStop()
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = s.secureHTTPServer.Shutdown(ctx)
					s.secureGRPCServer.Stop()
				}()
			}()
			return nil
		})
	}

	return nil
}

func (s *Server) initConsulRegistry(serviceControllers *aggregate.Controller, args *PilotArgs) error {
	log.Infof("Consul url: %v", args.Service.Consul.ServerURL)
	conctl, conerr := consul.NewController(
		args.Service.Consul.ServerURL)
	if conerr != nil {
		return fmt.Errorf("failed to create Consul controller: %v", conerr)
	}
	serviceControllers.AddRegistry(
		aggregate.Registry{
			Name:             serviceregistry.ConsulRegistry,
			ServiceDiscovery: conctl,
			Controller:       conctl,
		})

	return nil
}

func (s *Server) initGrpcServer(options *istiokeepalive.Options) {
	grpcOptions := s.grpcServerOptions(options)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.EnvoyXdsServer.Register(s.grpcServer)
}

// initialize secureGRPCServer
func (s *Server) initSecureGrpcServer(options *istiokeepalive.Options) error {
	certDir := features.CertDir
	if certDir == "" {
		certDir = PilotCertDir
	}

	ca := path.Join(certDir, constants.RootCertFilename)
	key := path.Join(certDir, constants.KeyFilename)
	cert := path.Join(certDir, constants.CertChainFilename)

	tlsCreds, err := credentials.NewServerTLSFromFile(cert, key)
	// certs not ready yet.
	if err != nil {
		return err
	}

	// TODO: parse the file to determine expiration date. Restart listener before expiration
	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	caCert, err := ioutil.ReadFile(ca)
	if err != nil {
		return err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	opts := s.grpcServerOptions(options)
	opts = append(opts, grpc.Creds(tlsCreds))
	s.secureGRPCServer = grpc.NewServer(opts...)
	s.EnvoyXdsServer.Register(s.secureGRPCServer)
	s.secureHTTPServer = &http.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				// For now accept any certs - pilot is not authenticating the caller, TLS used for
				// privacy
				return nil
			},
			NextProtos: []string{"h2", "http/1.1"},
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  caCertPool,
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(
				r.Header.Get("Content-Type"), "application/grpc") {
				s.secureGRPCServer.ServeHTTP(w, r)
			} else {
				s.mux.ServeHTTP(w, r)
			}
		}),
	}

	return nil
}

func (s *Server) grpcServerOptions(options *istiokeepalive.Options) []grpc.ServerOption {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}

	// Temp setting, default should be enough for most supported environments. Can be used for testing
	// envoy with lower values.
	maxStreams := features.MaxConcurrentStreams

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}

// Add to the FileWatcher the provided file and execute the provided function
// on any change event for this file.
// Using a debouncing mechanism to avoid calling the callback multiple times
// per event.
func (s *Server) addFileWatcher(file string, callback func()) {
	_ = s.fileWatcher.Add(file)
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-timerC:
				timerC = nil
				callback()
			case <-s.fileWatcher.Events(file):
				// Use a timer to debounce configuration updates
				if timerC == nil {
					timerC = time.After(100 * time.Millisecond)
				}
			}
		}
	}()
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	// TODO: remove dependency on k8s lib
	if !cache.WaitForCacheSync(stop, func() bool {
		if s.kubeRegistry != nil {
			if !s.kubeRegistry.HasSynced() {
				return false
			}
		}
		if !s.configController.HasSynced() {
			return false
		}
		return true
	}) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}

	return true
}

func (s *Server) initCertController(args *PilotArgs) error {
	var err error
	var secretNames, dnsNames, namespaces []string
	// Whether a key and cert are generated for Pilot
	var pilotCertGenerated bool

	if s.mesh.GetCertificates() == nil || len(s.mesh.GetCertificates()) == 0 {
		log.Info("nil certificate config")
		return nil
	}

	k8sClient := s.kubeClient
	for _, c := range s.mesh.GetCertificates() {
		name := strings.Join(c.GetDnsNames(), ",")
		if len(name) == 0 { // must have a DNS name
			continue
		}
		if len(c.GetSecretName()) > 0 { //
			// Chiron will generate the key and certificate and save them in a secret
			secretNames = append(secretNames, c.GetSecretName())
			dnsNames = append(dnsNames, name)
			namespaces = append(namespaces, args.Namespace)
		} else if !pilotCertGenerated {
			// Generate a key and certificate for the service and save them into a hard-coded directory.
			// Only one service (currently Pilot) will save the key and certificate in a directory.
			// Create directory at s.mesh.K8SCertificateSetting.PilotCertificatePath if it doesn't exist.
			svcName := "istio.pilot"
			userHomeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("could not find local user folder: %v", err)
			}
			dir := userHomeDir + DefaultDirectoryForKeyCert
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				err := os.MkdirAll(dir, os.ModePerm)
				if err != nil {
					return fmt.Errorf("err to create directory %v: %v", dir, err)
				}
			}
			// Generate Pilot certificate
			certChain, keyPEM, caCert, err := chiron.GenKeyCertK8sCA(k8sClient.CertificatesV1beta1().CertificateSigningRequests(),
				name, svcName+".csr.secret", args.Namespace, DefaultCACertPath)
			if err != nil {
				log.Errorf("err to generate key cert for %v: %v", svcName, err)
				return nil
			}
			// Save cert-chain.pem, root.pem, and key.pem to the directory.
			file := path.Join(dir, "cert-chain.pem")
			if err = ioutil.WriteFile(file, certChain, 0644); err != nil {
				log.Errorf("err to write %v cert-chain.pem (%v): %v", svcName, file, err)
				return nil
			}
			file = path.Join(dir, "root.pem")
			if err = ioutil.WriteFile(file, caCert, 0644); err != nil {
				log.Errorf("err to write %v root.pem (%v): %v", svcName, file, err)
				return nil
			}
			file = path.Join(dir, "key.pem")
			if err = ioutil.WriteFile(file, keyPEM, 0600); err != nil {
				log.Errorf("err to write %v key.pem (%v): %v", svcName, file, err)
				return nil
			}
			pilotCertGenerated = true
		}
	}

	// Provision and manage the certificates for non-Pilot services.
	// If services are empty, the certificate controller will do nothing.
	s.certController, err = chiron.NewWebhookController(DefaultCertGracePeriodRatio, DefaultMinCertGracePeriod,
		k8sClient.CoreV1(), k8sClient.AdmissionregistrationV1beta1(), k8sClient.CertificatesV1beta1(),
		DefaultCACertPath, secretNames, dnsNames, namespaces)
	if err != nil {
		return fmt.Errorf("failed to create certificate controller: %v", err)
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			// Run Chiron to manage the lifecycles of certificates
			s.certController.Run(stop)
		}()

		return nil
	})

	return nil
}

// initEventHandlers sets up event handlers for config and service updates
func (s *Server) initEventHandlers() error {
	// Flush cached discovery responses whenever services configuration change.
	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full:               true,
			NamespacesUpdated:  map[string]struct{}{svc.Attributes.Namespace: {}},
			ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
		}
		s.EnvoyXdsServer.ConfigUpdate(pushReq)
	}
	if err := s.ServiceController.AppendServiceHandler(serviceHandler); err != nil {
		return fmt.Errorf("append service handler failed: %v", err)
	}

	instanceHandler := func(si *model.ServiceInstance, _ model.Event) {
		// TODO: This is an incomplete code. This code path is called for service entries, consul, etc.
		// In all cases, this is simply an instance update and not a config update. So, we need to update
		// EDS in all proxies, and do a full config push for the instance that just changed (add/update only).
		s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{
			Full:              true,
			NamespacesUpdated: map[string]struct{}{si.Service.Attributes.Namespace: {}},
			// TODO: extend and set service instance type, so no need re-init push context
			ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
		})
	}
	if err := s.ServiceController.AppendInstanceHandler(instanceHandler); err != nil {
		return fmt.Errorf("append instance handler failed: %v", err)
	}

	// TODO(Nino-k): remove this case once incrementalUpdate is default
	if s.configController != nil {
		// TODO: changes should not trigger a full recompute of LDS/RDS/CDS/EDS
		// (especially mixerclient HTTP and quota)
		configHandler := func(c model.Config, _ model.Event) {
			pushReq := &model.PushRequest{
				Full:               true,
				ConfigTypesUpdated: map[string]struct{}{c.Type: {}},
			}
			s.EnvoyXdsServer.ConfigUpdate(pushReq)
		}
		for _, descriptor := range schemas.Istio {
			s.configController.RegisterEventHandler(descriptor.Type, configHandler)
		}
	}

	return nil
}
