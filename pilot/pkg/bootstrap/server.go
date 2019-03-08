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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gogo/protobuf/types"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	multierror "github.com/hashicorp/go-multierror"
	prom "github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	mcpapi "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	istio_networking_v1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/cmd"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
	istio_networking "istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/consul"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	srmemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/filewatcher"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/client"
	"istio.io/istio/pkg/mcp/configz"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"
	"istio.io/istio/pkg/version"
)

const (
	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"

	requiredMCPCertCheckFreq = 500 * time.Millisecond

	// DefaultMCPMaxMsgSize is the default maximum message size
	DefaultMCPMaxMsgSize = 1024 * 1024 * 4

	// URL types supported by the config store
	// example fs:///tmp/configroot
	fsScheme = "fs"
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
	ConfigFile      string
	MixerAddress    string
	RdsRefreshDelay *types.Duration
}

// ConfigArgs provide configuration options for the configuration controller. If FileDir is set, that directory will
// be monitored for CRD yaml files and will update the controller as those files change (This is used for testing
// purposes). Otherwise, a CRD client is created based on the configuration.
type ConfigArgs struct {
	ClusterRegistriesNamespace string
	KubeConfig                 string
	ControllerOptions          kube.ControllerOptions
	FileDir                    string
	DisableInstallCRDs         bool

	// Controller if specified, this controller overrides the other config settings.
	Controller model.ConfigStoreCache
}

// ConsulArgs provides configuration for the Consul service registry.
type ConsulArgs struct {
	Config    string
	ServerURL string
	Interval  time.Duration
}

// ServiceArgs provides the composite configuration for all service registries in the system.
type ServiceArgs struct {
	Registries []string
	Consul     ConsulArgs
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	DiscoveryOptions     envoy.DiscoveryServiceOptions
	Namespace            string
	Mesh                 MeshArgs
	Config               ConfigArgs
	Service              ServiceArgs
	MeshConfig           *meshconfig.MeshConfig
	NetworksConfigFile   string
	CtrlZOptions         *ctrlz.Options
	Plugins              []string
	MCPServerAddrs       []string
	MCPCredentialOptions *creds.Options
	MCPMaxMessageSize    int
	KeepaliveOptions     *istiokeepalive.Options
	// ForceStop is set as true when used for testing to make the server stop quickly
	ForceStop bool
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
	mixerSAN         []string
	kubeClient       kubernetes.Interface
	startFuncs       []startFunc
	multicluster     *clusterregistry.Multicluster
	httpServer       *http.Server
	grpcServer       *grpc.Server
	secureHTTPServer *http.Server
	secureGRPCServer *grpc.Server
	istioConfigStore model.IstioConfigStore
	mux              *http.ServeMux
	kubeRegistry     *kube.Controller
	fileWatcher      filewatcher.FileWatcher
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args PilotArgs) (*Server, error) {
	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = os.Getenv("POD_NAMESPACE")
	}
	if args.KeepaliveOptions == nil {
		args.KeepaliveOptions = istiokeepalive.DefaultOption()
	}
	if args.Config.ClusterRegistriesNamespace == "" {
		if args.Namespace != "" {
			args.Config.ClusterRegistriesNamespace = args.Namespace
		} else {
			args.Config.ClusterRegistriesNamespace = model.IstioSystemNamespace
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
	if err := s.initMixerSan(&args); err != nil {
		return nil, fmt.Errorf("mixer san: %v", err)
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
func (s *Server) initMonitor(args *PilotArgs) error {
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
		defaultMesh := model.DefaultMeshConfig()
		return nil, &defaultMesh, nil
	}

	config, err := kube.CoreV1().ConfigMaps(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			defaultMesh := model.DefaultMeshConfig()
			return nil, &defaultMesh, nil
		}
		return nil, nil, err
	}

	// values in the data are strings, while proto might use a different data type.
	// therefore, we have to get a value by a key
	cfgYaml, exists := config.Data[ConfigMapKey]
	if !exists {
		return nil, nil, fmt.Errorf("missing configuration map key %q", ConfigMapKey)
	}

	mesh, err := model.ApplyMeshConfigDefaults(cfgYaml)
	if err != nil {
		return nil, nil, err
	}
	return config, mesh, nil
}

// initMesh creates the mesh in the pilotConfig from the input arguments.
func (s *Server) initMesh(args *PilotArgs) error {
	// If a config file was specified, use it.
	if args.MeshConfig != nil {
		s.mesh = args.MeshConfig
		return nil
	}
	var mesh *meshconfig.MeshConfig
	var err error

	if args.Mesh.ConfigFile != "" {
		mesh, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
		}

		// Watch the config file for changes and reload if it got modified
		s.addFileWatcher(args.Mesh.ConfigFile, func() {
			// Reload the config file
			mesh, err = cmd.ReadMeshConfig(args.Mesh.ConfigFile)
			if err != nil {
				log.Warnf("failed to read mesh configuration, using default: %v", err)
				return
			}
			if !reflect.DeepEqual(mesh, s.mesh) {
				log.Infof("mesh configurtion file updated to: %s", spew.Sdump(mesh))
				if !reflect.DeepEqual(mesh.ConfigSources, s.mesh.ConfigSources) {
					log.Infof("mesh configuration sources have changed")
					//TODO Need to re-create or reload initConfigController()
				}
				s.mesh = mesh
				if s.EnvoyXdsServer != nil {
					s.EnvoyXdsServer.Env.Mesh = mesh
					s.EnvoyXdsServer.ConfigUpdate(true)
				}
			}
		})
	}

	if mesh == nil {
		// Config file either wasn't specified or failed to load - use a default mesh.
		if _, mesh, err = GetMeshConfig(s.kubeClient, kube.IstioNamespace, kube.IstioConfigMap); err != nil {
			log.Warnf("failed to read the default mesh configuration: %v, from the %s config map in the %s namespace", err, kube.IstioConfigMap, kube.IstioNamespace)
			return err
		}

		// Allow some overrides for testing purposes.
		if args.Mesh.MixerAddress != "" {
			mesh.MixerCheckServer = args.Mesh.MixerAddress
			mesh.MixerReportServer = args.Mesh.MixerAddress
		}
	}

	log.Infof("mesh configuration %s", spew.Sdump(mesh))
	log.Infof("version %s", version.Info.String())
	log.Infof("flags %s", spew.Sdump(args))

	s.mesh = mesh
	return nil
}

// initMeshNetworks loads the mesh networks configuration from the file provided
// in the args and add a watcher for changes in this file.
func (s *Server) initMeshNetworks(args *PilotArgs) error {
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
	util.ResolveHostsInNetworksConfig(s.meshNetworks)
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
			util.ResolveHostsInNetworksConfig(s.meshNetworks)
			s.meshNetworks = meshNetworks
			if s.kubeRegistry != nil {
				s.kubeRegistry.InitNetworkLookup(meshNetworks)
			}
			if s.EnvoyXdsServer != nil {
				s.EnvoyXdsServer.Env.MeshNetworks = meshNetworks
				s.EnvoyXdsServer.ConfigUpdate(true)
			}
		}
	})

	return nil
}

// initMixerSan configures the mixerSAN configuration item. The mesh must already have been configured.
func (s *Server) initMixerSan(args *PilotArgs) error {
	if s.mesh == nil {
		return fmt.Errorf("the mesh has not been configured before configuring mixer spiffe")
	}
	if s.mesh.DefaultConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		s.mixerSAN = []string{envoy.GetMixerSAN(args.Namespace)}
	}
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
	clientNodeID := ""
	collections := make([]sink.CollectionOptions, len(model.IstioConfigTypes))
	for i, model := range model.IstioConfigTypes {
		collections[i] = sink.CollectionOptions{
			Name: model.Collection,
		}
	}

	options := coredatamodel.Options{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
		ClearDiscoveryServerCache: func() {
			s.EnvoyXdsServer.ConfigUpdate(true)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var clients []*client.Client
	var clients2 []*sink.Client
	var conns []*grpc.ClientConn
	var configStores []model.ConfigStoreCache

	// TODO - temporarily support both the new and old stack during transition
	var useLegacyMCPStack bool
	if os.Getenv("USE_MCP_LEGACY") == "1" {
		useLegacyMCPStack = true
		log.Infof("USE_MCP_LEGACY=1 - using legacy MCP client stack")
	} else {
		log.Infof("Using new MCP client sink stack")
	}

	reporter := monitoring.NewStatsContext("pilot/mcp/sink")

	for _, configSource := range s.mesh.ConfigSources {
		url, err := url.Parse(configSource.Address)
		if err != nil {
			cancel()
			return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
		}
		if url.Scheme == fsScheme {
			if url.Path == "" {
				cancel()
				return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
			}
			store := memory.Make(model.IstioConfigTypes)
			configController := memory.NewController(store)

			err := s.makeFileMonitor(url.Path, configController)
			if err != nil {
				cancel()
				return err
			}
			configStores = append(configStores, configController)
			continue
		}

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
					CertificateFile:   path.Join(model.AuthCertsPath, model.CertChainFilename),
					KeyFile:           path.Join(model.AuthCertsPath, model.KeyFilename),
					CACertificateFile: path.Join(model.AuthCertsPath, model.RootCertFilename),
				}
			default:
				log.Errorf("invalid tls setting mode %d", configSource.TlsSettings.Mode)
				continue
			}

			if credentialOption == nil {
				credentials := creds.CreateForClientSkipVerify()
				securityOption = grpc.WithTransportCredentials(credentials)
			} else {
				requiredFiles := []string{credentialOption.CACertificateFile, credentialOption.KeyFile, credentialOption.CertificateFile}
				log.Infof("Secure MCP configured. Waiting for required certificate files to become available: %v",
					requiredFiles)
				for len(requiredFiles) > 0 {
					if _, err := os.Stat(requiredFiles[0]); os.IsNotExist(err) {
						log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredMCPCertCheckFreq)
						select {
						case <-ctx.Done():
							cancel()
							return ctx.Err()
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
					return err
				}
				credentials := creds.CreateForClient(configSource.TlsSettings.Sni, watcher)
				securityOption = grpc.WithTransportCredentials(credentials)
			}
		}

		keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    args.KeepaliveOptions.Time,
			Timeout: args.KeepaliveOptions.Timeout,
		})
		msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(args.MCPMaxMessageSize))
		conn, err := grpc.DialContext(ctx, configSource.Address, securityOption, msgSizeOption, keepaliveOption)
		if err != nil {
			log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
			cancel()
			return err
		}

		mcpController := coredatamodel.NewController(options)
		sinkOptions := &sink.Options{
			CollectionOptions: collections,
			Updater:           mcpController,
			ID:                clientNodeID,
			Reporter:          reporter,
		}

		if useLegacyMCPStack {
			cl := mcpapi.NewAggregatedMeshConfigServiceClient(conn)
			mcpClient := client.New(cl, sinkOptions)
			configz.Register(mcpClient)
			clients = append(clients, mcpClient)
		} else {
			cl2 := mcpapi.NewResourceSourceClient(conn)
			mcpClient2 := sink.NewClient(cl2, sinkOptions)
			configz.Register(mcpClient2)
			clients2 = append(clients2, mcpClient2)
		}

		conns = append(conns, conn)
		configStores = append(configStores, mcpController)
	}

	// TODO: remove the below branch when `--mcpServerAddrs` removed
	if len(configStores) == 0 {
		for _, addr := range args.MCPServerAddrs {
			u, err := url.Parse(addr)
			if err != nil {
				cancel()
				return err
			}

			securityOption := grpc.WithInsecure()
			if u.Scheme == "mcps" {
				requiredFiles := []string{
					args.MCPCredentialOptions.CertificateFile,
					args.MCPCredentialOptions.KeyFile,
					args.MCPCredentialOptions.CACertificateFile,
				}
				log.Infof("Secure MCP configured. Waiting for required certificate files to become available: %v",
					requiredFiles)
				for len(requiredFiles) > 0 {
					if _, err := os.Stat(requiredFiles[0]); os.IsNotExist(err) {
						log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredMCPCertCheckFreq)
						select {
						case <-ctx.Done():
							cancel()
							return ctx.Err()
						case <-time.After(requiredMCPCertCheckFreq):
							// retry
						}
						continue
					}
					log.Infof("%v found", requiredFiles[0])
					requiredFiles = requiredFiles[1:]
				}

				watcher, err := creds.WatchFiles(ctx.Done(), args.MCPCredentialOptions)
				if err != nil {
					cancel()
					return err
				}
				credentials := creds.CreateForClient(u.Hostname(), watcher)
				securityOption = grpc.WithTransportCredentials(credentials)
			}
			msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(args.MCPMaxMessageSize))
			conn, err := grpc.DialContext(ctx, u.Host, securityOption, msgSizeOption)
			if err != nil {
				log.Errorf("Unable to dial MCP Server %q: %v", u.Host, err)
				cancel()
				return err
			}

			mcpController := coredatamodel.NewController(options)
			sinkOptions := &sink.Options{
				CollectionOptions: collections,
				Updater:           mcpController,
				ID:                clientNodeID,
				Reporter:          reporter,
			}

			if useLegacyMCPStack {
				cl := mcpapi.NewAggregatedMeshConfigServiceClient(conn)
				mcpClient := client.New(cl, sinkOptions)
				configz.Register(mcpClient)
				clients = append(clients, mcpClient)
			} else {
				cl2 := mcpapi.NewResourceSourceClient(conn)
				mcpClient2 := sink.NewClient(cl2, sinkOptions)
				configz.Register(mcpClient2)
				clients2 = append(clients2, mcpClient2)
			}

			conns = append(conns, conn)
			configStores = append(configStores, mcpController)
		}
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		var wg sync.WaitGroup

		if useLegacyMCPStack {
			for i := range clients {
				client := clients[i]
				wg.Add(1)
				go func() {
					client.Run(ctx)
					wg.Done()
				}()
			}
		} else {
			for i := range clients2 {
				client := clients2[i]
				wg.Add(1)
				go func() {
					client.Run(ctx)
					wg.Done()
				}()
			}
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

			reporter.Close()
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

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
	if len(args.MCPServerAddrs) > 0 || len(s.mesh.ConfigSources) > 0 {
		if err := s.initMCPConfigController(args); err != nil {
			return err
		}
	} else if args.Config.Controller != nil {
		s.configController = args.Config.Controller
	} else if args.Config.FileDir != "" {
		store := memory.Make(model.IstioConfigTypes)
		configController := memory.NewController(store)

		err := s.makeFileMonitor(args.Config.FileDir, configController)
		if err != nil {
			return err
		}

		s.configController = configController
	} else {
		controller, err := s.makeKubeConfigController(args)
		if err != nil {
			return err
		}

		s.configController = controller
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
	configClient, err := crd.NewClient(kubeCfgFile, "", model.IstioConfigTypes, args.Config.ControllerOptions.DomainSuffix)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if !args.Config.DisableInstallCRDs {
		if err = configClient.RegisterResources(); err != nil {
			return nil, multierror.Prefix(err, "failed to register custom resources.")
		}
	}

	return crd.NewController(configClient, args.Config.ControllerOptions), nil
}

func (s *Server) makeFileMonitor(fileDir string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, model.IstioConfigTypes)
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
	kubectl := kube.NewController(s.kubeClient, args.Config.ControllerOptions)
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
			log.Infof("no-op: get service info from MCP ServiceEntries.")
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
		map[model.Hostname]*model.Service{ // srmemory.HelloService.Hostname: srmemory.HelloService,
		}, 2)

	discovery2 := srmemory.NewDiscovery(
		map[model.Hostname]*model.Service{ // srmemory.WorldService.Hostname: srmemory.WorldService,
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
		MixerSAN:         s.mixerSAN,
	}

	// Set up discovery service
	discovery, err := envoy.NewDiscoveryService(
		environment,
		args.DiscoveryOptions,
	)
	if err != nil {
		return fmt.Errorf("failed to create discovery service: %v", err)
	}
	s.mux = discovery.RestContainer.ServeMux

	s.EnvoyXdsServer = envoyv2.NewDiscoveryServer(environment,
		istio_networking.NewConfigGenerator(args.Plugins),
		s.ServiceController, s.kubeRegistry, s.configController)
	s.EnvoyXdsServer.InitDebug(s.mux, s.ServiceController)
	if s.kubeRegistry != nil {
		// kubeRegistry may use the environment for push status reporting.
		// TODO: maybe all registries should have this as an optional field ?
		s.kubeRegistry.Env = environment
		s.kubeRegistry.InitNetworkLookup(s.meshNetworks)
		s.kubeRegistry.XDSUpdater = s.EnvoyXdsServer
	}

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
		go func() {
			if pilot.EnableWaitCacheSync && !s.waitForCacheSync(stop) {
				return
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
				if pilot.EnableWaitCacheSync && !s.waitForCacheSync(stop) {
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
					s.secureHTTPServer.Shutdown(ctx)
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
		args.Service.Consul.ServerURL, args.Service.Consul.Interval)
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
	certDir := pilot.CertDir
	if certDir == "" {
		certDir = PilotCertDir
	}

	ca := path.Join(certDir, model.RootCertFilename)
	key := path.Join(certDir, model.KeyFilename)
	cert := path.Join(certDir, model.CertChainFilename)

	creds, err := credentials.NewServerTLSFromFile(cert, key)
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
	opts = append(opts, grpc.Creds(creds))
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
	var maxStreams int
	maxStreamsEnv := pilot.MaxConcurrentStreams
	if len(maxStreamsEnv) > 0 {
		maxStreams, _ = strconv.Atoi(maxStreamsEnv)
	}
	if maxStreams == 0 {
		maxStreams = 100000
	}

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
	s.fileWatcher.Add(file)
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
