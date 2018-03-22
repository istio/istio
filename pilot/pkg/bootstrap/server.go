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
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"code.cloudfoundry.org/copilot"
	"github.com/davecgh/go-spew/spew"
	durpb "github.com/golang/protobuf/ptypes/duration"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	multierror "github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/kube/admit"
	"istio.io/istio/pilot/pkg/model"
	envoy "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
	"istio.io/istio/pilot/pkg/serviceregistry/consul"
	"istio.io/istio/pilot/pkg/serviceregistry/eureka"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

// ServiceRegistry is an expansion of the platform.ServiceRegistry enum that adds a mock registry.
type ServiceRegistry string

const (
	// MockRegistry environment flag
	MockRegistry ServiceRegistry = "Mock"
	// KubernetesRegistry environment flag
	KubernetesRegistry ServiceRegistry = "Kubernetes"
	// ConsulRegistry environment flag
	ConsulRegistry ServiceRegistry = "Consul"
	// EurekaRegistry environment flag
	EurekaRegistry ServiceRegistry = "Eureka"
	// CloudFoundryRegistry environment flag
	CloudFoundryRegistry ServiceRegistry = "CloudFoundry"
	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"
	// CopilotTimeout when to cancel remote gRPC call to copilot
	CopilotTimeout = 5 * time.Second
	// FilepathWalkInterval dictates how often the file system is walked for config
	FilepathWalkInterval = 100 * time.Millisecond
)

var (
	// TODO: use model.IstioConfigTypes once model.IngressRule is deprecated
	configDescriptor = model.ConfigDescriptor{
		model.RouteRule,
		model.VirtualService,
		model.Gateway,
		model.EgressRule,
		model.ExternalService,
		model.DestinationPolicy,
		model.DestinationRule,
		model.HTTPAPISpec,
		model.HTTPAPISpecBinding,
		model.QuotaSpec,
		model.QuotaSpecBinding,
		model.EndUserAuthenticationPolicySpec,
		model.EndUserAuthenticationPolicySpecBinding,
		model.AuthenticationPolicy,
		model.ServiceRole,
		model.ServiceRoleBinding,
	}
)

// MeshArgs provide configuration options for the mesh. If ConfigFile is provided, an attempt will be made to
// load the mesh from the file. Otherwise, a default mesh will be used with optional overrides.
type MeshArgs struct {
	ConfigFile      string
	MixerAddress    string
	RdsRefreshDelay *durpb.Duration
}

// ConfigArgs provide configuration options for the configuration controller. If FileDir is set, that directory will
// be monitored for CRD yaml files and will update the controller as those files change (This is used for testing
// purposes). Otherwise, a CRD client is created based on the configuration.
type ConfigArgs struct {
	ClusterRegistriesDir string
	KubeConfig           string
	CFConfig             string
	ControllerOptions    kube.ControllerOptions
	FileDir              string
}

// ConsulArgs provides configuration for the Consul service registry.
type ConsulArgs struct {
	Config    string
	ServerURL string
	Interval  time.Duration
}

// EurekaArgs provides configuration for the Eureka service registry
type EurekaArgs struct {
	ServerURL string
	Interval  time.Duration
}

// ServiceArgs provides the composite configuration for all service registries in the system.
type ServiceArgs struct {
	Registries []string
	Consul     ConsulArgs
	Eureka     EurekaArgs
}

// AdmissionArgs provides configuration options for the admission controller. This is a partial duplicate of
// admit.ControllerOptions (other fields are filled out before constructing the admission controller). Only
// used if running with k8s, Consul, or Eureka (not in a mock environment).
type AdmissionArgs struct {
	// ExternalAdmissionWebhookName is the name of the
	// ExternalAdmissionHook which describes he external admission
	// webhook and resources and operations it applies to.
	ExternalAdmissionWebhookName string

	// ServiceName is the service name of the webhook.
	ServiceName string

	// SecretName is the name of k8s secret that contains the webhook
	// server key/cert and corresponding CA cert that signed them. The
	// server key/cert are used to serve the webhook and the CA cert
	// is provided to k8s apiserver during admission controller
	// registration.
	SecretName string

	// Port where the webhook is served. Per k8s admission
	// registration requirements this should be 443 unless there is
	// only a single port for the service.
	Port int

	// RegistrationDelay controls how long admission registration
	// occurs after the webhook is started. This is used to avoid
	// potential races where registration completes and k8s apiserver
	// invokes the webhook before the HTTP server is started.
	RegistrationDelay time.Duration
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	DiscoveryOptions envoy.DiscoveryServiceOptions
	Namespace        string
	Mesh             MeshArgs
	Config           ConfigArgs
	Service          ServiceArgs
	Admission        AdmissionArgs
	RDSv2            bool
}

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	mesh              *meshconfig.MeshConfig
	ServiceController *aggregate.Controller
	configController  model.ConfigStoreCache
	mixerSAN          []string
	kubeClient        kubernetes.Interface
	startFuncs        []startFunc
	HTTPListeningAddr net.Addr
	GRPCListeningAddr net.Addr
	clusterStore      *clusterregistry.ClusterStore

	EnvoyXdsServer   *envoyv2.DiscoveryServer
	HTTPServer       *http.Server
	GRPCServer       *grpc.Server
	DiscoveryService *envoy.DiscoveryService

	// An in-memory service discovery, enabled if 'mock' registry is added.
	// Currently used for tests.
	MemoryServiceDiscovery *mock.ServiceDiscovery
}

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args PilotArgs) (*Server, error) {
	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = os.Getenv("POD_NAMESPACE")
	}

	// TODO: remove when no longer needed
	if os.Getenv("PILOT_VALIDATE_CLUSTERS") == "false" {
		envoy.ValidateClusters = false
	}

	s := &Server{}

	// Apply the arguments to the configuration.
	if err := s.initMonitor(&args); err != nil {
		return nil, err
	}
	if err := s.initClusterRegistries(&args); err != nil {
		return nil, err
	}
	if err := s.initKubeClient(&args); err != nil {
		return nil, err
	}
	if err := s.initMesh(&args); err != nil {
		return nil, err
	}
	if err := s.initAdmissionController(&args); err != nil {
		return nil, err
	}
	if err := s.initMixerSan(&args); err != nil {
		return nil, err
	}
	if err := s.initConfigController(&args); err != nil {
		return nil, err
	}
	if err := s.initServiceControllers(&args); err != nil {
		return nil, err
	}
	if err := s.initDiscoveryService(&args); err != nil {
		return nil, err
	}
	return s, nil
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServiceOptions.
// If Port == 0, a port number is automatically chosen. This method returns the address on which the server is
// listening for incoming connections. Content serving is started by this method, but is executed asynchronously.
// Serving can be cancelled at any time by closing the provided stop channel.
func (s *Server) Start(stop chan struct{}) (net.Addr, error) {
	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return nil, err
		}
	}

	return s.HTTPListeningAddr, nil
}

// startFunc defines a function that will be used to start one or more components of the Pilot discovery service.
type startFunc func(stop chan struct{}) error

// initMonitor initializes the configuration for the pilot monitoring server.
func (s *Server) initMonitor(args *PilotArgs) error {
	s.addStartFunc(func(stop chan struct{}) error {
		monitor, err := startMonitor(args.DiscoveryOptions.MonitoringPort)
		if err != nil {
			return err
		}

		go func() {
			<-stop
			err := monitor.Close()
			log.Debugf("Monitoring server terminated: %v", err)
		}()
		return nil
	})
	return nil
}

func (s *Server) initClusterRegistries(args *PilotArgs) (err error) {
	if args.Config.ClusterRegistriesDir != "" {
		s.clusterStore, err = clusterregistry.ReadClusters(args.Config.ClusterRegistriesDir)
		if s.clusterStore != nil {
			log.Infof("clusters configuration %s", spew.Sdump(s.clusterStore))
		}
	}
	return err
}

// GetMeshConfig fetches the ProxyMesh configuration from Kubernetes ConfigMap.
func GetMeshConfig(kube kubernetes.Interface, namespace, name string) (*v1.ConfigMap, *meshconfig.MeshConfig, error) {

	if kube == nil {
		defaultMesh := model.DefaultMeshConfig()
		return nil, &defaultMesh, nil
	}

	config, err := kube.CoreV1().ConfigMaps(namespace).Get(name, meta_v1.GetOptions{})
	if err != nil {
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
	var mesh *meshconfig.MeshConfig
	if args.Mesh.ConfigFile != "" {
		fileMesh, err := cmd.ReadMeshConfig(args.Mesh.ConfigFile)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
		} else {
			mesh = fileMesh
		}
	}

	if mesh == nil {
		var err error
		// Config file either wasn't specified or failed to load - use a default mesh.
		if _, mesh, err = GetMeshConfig(s.kubeClient, kube.IstioNamespace, kube.IstioConfigMap); err != nil {
			log.Warnf("failed to read mesh configuration: %v", err)
			return err
		}

		// Allow some overrides for testing purposes.
		if args.Mesh.MixerAddress != "" {
			mesh.MixerCheckServer = args.Mesh.MixerAddress
			mesh.MixerReportServer = args.Mesh.MixerAddress
		}
		if args.Mesh.RdsRefreshDelay != nil {
			mesh.RdsRefreshDelay = args.Mesh.RdsRefreshDelay
		}
	}

	log.Infof("mesh configuration %s", spew.Sdump(mesh))
	log.Infof("version %s", version.Info.String())
	log.Infof("flags %s", spew.Sdump(args))

	s.mesh = mesh
	return nil
}

// initMixerSan configures the mixerSAN configuration item. The mesh must already have been configured.
func (s *Server) initMixerSan(args *PilotArgs) error {
	if s.mesh == nil {
		return fmt.Errorf("the mesh has not been configured before configuring mixer san")
	}
	if s.mesh.DefaultConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		s.mixerSAN = envoy.GetMixerSAN(args.Config.ControllerOptions.DomainSuffix, args.Namespace)
	}
	return nil
}

func (s *Server) getKubeCfgFile(args *PilotArgs) (kubeCfgFile string) {
	// If the cluster store is configured, get pilot's kubeconfig from there
	if s.clusterStore != nil {
		if kubeCfgFile = s.clusterStore.GetPilotAccessConfig(); kubeCfgFile != "" {
			kubeCfgFile = path.Join(args.Config.ClusterRegistriesDir, kubeCfgFile)
		}
	}
	if kubeCfgFile == "" {
		kubeCfgFile = args.Config.KubeConfig
	}
	return
}

// initKubeClient creates the k8s client if running in an k8s environment.
func (s *Server) initKubeClient(args *PilotArgs) error {
	needToCreateClient := false
	for _, r := range args.Service.Registries {
		switch ServiceRegistry(r) {
		case KubernetesRegistry:
			needToCreateClient = true
		case ConsulRegistry:
			needToCreateClient = true
		case EurekaRegistry:
			needToCreateClient = true
		}
	}

	if needToCreateClient && args.Config.FileDir == "" {
		var client kubernetes.Interface
		var kuberr error

		kubeCfgFile := s.getKubeCfgFile(args)
		_, client, kuberr = kube.CreateInterface(kubeCfgFile)
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

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
	if args.Config.FileDir != "" {
		store := memory.Make(configDescriptor)
		configController := memory.NewController(store)

		err := s.makeFileMonitor(args, configController)
		if err != nil {
			return err
		}

		if args.Config.CFConfig != "" {
			err = s.makeCopilotMonitor(args, configController)
			if err != nil {
				return err
			}
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
	s.addStartFunc(func(stop chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	kubeCfgFile := s.getKubeCfgFile(args)
	configClient, err := crd.NewClient(kubeCfgFile, configDescriptor, args.Config.ControllerOptions.DomainSuffix)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if err = configClient.RegisterResources(); err != nil {
		return nil, multierror.Prefix(err, "failed to register custom resources.")
	}

	return crd.NewController(configClient, args.Config.ControllerOptions), nil
}

func (s *Server) makeFileMonitor(args *PilotArgs, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(args.Config.FileDir, configDescriptor)
	fileMonitor := configmonitor.NewMonitor(configController, FilepathWalkInterval, fileSnapshot.ReadConfigFiles)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc(func(stop chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}

func (s *Server) makeCopilotMonitor(args *PilotArgs, configController model.ConfigStore) error {
	cfConfig, err := cloudfoundry.LoadConfig(args.Config.CFConfig)
	if err != nil {
		return multierror.Prefix(err, "loading cloud foundry config")
	}
	tlsConfig, err := cfConfig.ClientTLSConfig()
	if err != nil {
		return multierror.Prefix(err, "creating cloud foundry client tls config")
	}
	client, err := copilot.NewIstioClient(cfConfig.Copilot.Address, tlsConfig)
	if err != nil {
		return multierror.Prefix(err, "creating cloud foundry client")
	}

	copilotSnapshot := configmonitor.NewCopilotSnapshot(configController, client, []string{".internal"}, CopilotTimeout)
	copilotMonitor := configmonitor.NewMonitor(configController, 1*time.Second, copilotSnapshot.ReadConfigFiles)

	s.addStartFunc(func(stop chan struct{}) error {
		copilotMonitor.Start(stop)
		return nil
	})

	return nil
}

// createK8sServiceControllers creates all the k8s service controllers under this pilot
func (s *Server) createK8sServiceControllers(serviceControllers *aggregate.Controller, args *PilotArgs) (err error) {
	kubectl := kube.NewController(s.kubeClient, args.Config.ControllerOptions)
	serviceControllers.AddRegistry(
		aggregate.Registry{
			Name:             serviceregistry.ServiceRegistry(KubernetesRegistry),
			ServiceDiscovery: kubectl,
			ServiceAccounts:  kubectl,
			Controller:       kubectl,
		})

	// Add clusters under the same pilot
	if s.clusterStore != nil {
		clusters := s.clusterStore.GetPilotClusters()
		for _, cluster := range clusters {
			kubeconfig := clusterregistry.GetClusterAccessConfig(cluster)
			kubeCfgFile := path.Join(args.Config.ClusterRegistriesDir, kubeconfig)
			log.Infof("Cluster name: %s, AccessConfigFile: %s", clusterregistry.GetClusterName(cluster), kubeCfgFile)
			_, client, kuberr := kube.CreateInterface(kubeCfgFile)
			if kuberr != nil {
				err = multierror.Append(err, multierror.Prefix(kuberr, fmt.Sprintf("failed to connect to Access API with accessconfig: %s", kubeCfgFile)))
			}

			kubectl := kube.NewController(client, args.Config.ControllerOptions)
			serviceControllers.AddRegistry(
				aggregate.Registry{
					Name:             serviceregistry.ServiceRegistry(KubernetesRegistry),
					ClusterName:      clusterregistry.GetClusterName(cluster),
					ServiceDiscovery: kubectl,
					ServiceAccounts:  kubectl,
					Controller:       kubectl,
				})
		}
	}
	return
}

// initServiceControllers creates and initializes the service controllers
func (s *Server) initServiceControllers(args *PilotArgs) error {
	serviceControllers := aggregate.NewController()
	registered := make(map[ServiceRegistry]bool)
	for _, r := range args.Service.Registries {
		serviceRegistry := ServiceRegistry(r)
		if _, exists := registered[serviceRegistry]; exists {
			log.Warnf("%s registry specified multiple times.", r)
			continue
		}
		registered[serviceRegistry] = true
		log.Infof("Adding %s registry adapter", serviceRegistry)
		switch serviceRegistry {
		case MockRegistry:
			initMemoryRegistry(s, serviceControllers)
		case KubernetesRegistry:
			if err := s.createK8sServiceControllers(serviceControllers, args); err != nil {
				return err
			}
			if s.mesh.IngressControllerMode != meshconfig.MeshConfig_OFF {
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
					s.addStartFunc(func(stop chan struct{}) error {
						go ingressSyncer.Run(stop)
						return nil
					})
				}
			}
		case ConsulRegistry:
			log.Infof("Consul url: %v", args.Service.Consul.ServerURL)
			conctl, conerr := consul.NewController(
				args.Service.Consul.ServerURL, args.Service.Consul.Interval)
			if conerr != nil {
				return fmt.Errorf("failed to create Consul controller: %v", conerr)
			}
			serviceControllers.AddRegistry(
				aggregate.Registry{
					Name:             serviceregistry.ServiceRegistry(r),
					ServiceDiscovery: conctl,
					ServiceAccounts:  conctl,
					Controller:       conctl,
				})
		case EurekaRegistry:
			log.Infof("Eureka url: %v", args.Service.Eureka.ServerURL)
			eurekaClient := eureka.NewClient(args.Service.Eureka.ServerURL)
			serviceControllers.AddRegistry(
				aggregate.Registry{
					Name:             serviceregistry.ServiceRegistry(r),
					Controller:       eureka.NewController(eurekaClient, args.Service.Eureka.Interval),
					ServiceDiscovery: eureka.NewServiceDiscovery(eurekaClient),
					ServiceAccounts:  eureka.NewServiceAccounts(),
				})

		case CloudFoundryRegistry:
			cfConfig, err := cloudfoundry.LoadConfig(args.Config.CFConfig)
			if err != nil {
				return multierror.Prefix(err, "loading cloud foundry config")
			}
			tlsConfig, err := cfConfig.ClientTLSConfig()
			if err != nil {
				return multierror.Prefix(err, "creating cloud foundry client tls config")
			}
			client, err := copilot.NewIstioClient(cfConfig.Copilot.Address, tlsConfig)
			if err != nil {
				return multierror.Prefix(err, "creating cloud foundry client")
			}
			serviceControllers.AddRegistry(aggregate.Registry{
				Name: serviceregistry.ServiceRegistry(r),
				Controller: &cloudfoundry.Controller{
					Ticker: cloudfoundry.NewTicker(cfConfig.Copilot.PollInterval),
					Client: client,
				},
				ServiceDiscovery: &cloudfoundry.ServiceDiscovery{
					Client:      client,
					ServicePort: cfConfig.ServicePort,
				},
				ServiceAccounts: cloudfoundry.NewServiceAccounts(),
			})

		default:
			return multierror.Prefix(nil, "Service registry "+r+" is not supported.")
		}
	}
	configStore := model.MakeIstioStore(s.configController)

	// add external service registry to aggregator by default
	serviceControllers.AddRegistry(
		aggregate.Registry{
			Name:             "ExternalServices",
			Controller:       external.NewController(s.configController),
			ServiceDiscovery: external.NewServiceDiscovery(configStore),
			ServiceAccounts:  external.NewServiceAccounts(),
		})

	s.ServiceController = serviceControllers

	// Defer running of the service controllers.
	s.addStartFunc(func(stop chan struct{}) error {
		go s.ServiceController.Run(stop)
		return nil
	})

	return nil
}
func initMemoryRegistry(s *Server, serviceControllers *aggregate.Controller) {
	// ServiceDiscovery implementation
	discovery1 := mock.NewDiscovery(
		map[string]*model.Service{
			//			mock.HelloService.Hostname: mock.HelloService,
		}, 2)

	s.MemoryServiceDiscovery = discovery1

	discovery2 := mock.NewDiscovery(
		map[string]*model.Service{
			//			mock.WorldService.Hostname: mock.WorldService,
		}, 2)

	registry1 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockAdapter1"),
		ServiceDiscovery: discovery1,
		ServiceAccounts:  discovery1,
		Controller:       &mockController{},
	}

	registry2 := aggregate.Registry{
		Name:             serviceregistry.ServiceRegistry("mockAdapter2"),
		ServiceDiscovery: discovery2,
		ServiceAccounts:  discovery2,
		Controller:       &mockController{},
	}
	serviceControllers.AddRegistry(registry1)
	serviceControllers.AddRegistry(registry2)
}

func (s *Server) initDiscoveryService(args *PilotArgs) error {
	environment := model.Environment{
		Mesh:             s.mesh,
		IstioConfigStore: model.MakeIstioStore(s.configController),
		ServiceDiscovery: s.ServiceController,
		ServiceAccounts:  s.ServiceController,
		MixerSAN:         s.mixerSAN,
	}

	// Set up discovery service
	discovery, err := envoy.NewDiscoveryService(
		s.ServiceController,
		s.configController,
		environment,
		args.DiscoveryOptions,
	)
	if err != nil {
		return fmt.Errorf("failed to create discovery service: %v", err)
	}
	s.DiscoveryService = discovery

	// For now we create the gRPC server sourcing data from Pilot's older data model.
	s.initGrpcServer()
	envoy.V2ClearCache = envoyv2.EdsPushAll
	s.EnvoyXdsServer = envoyv2.NewDiscoveryServer(discovery, s.GRPCServer, environment)

	s.HTTPServer = &http.Server{
		Addr:    ":" + strconv.Itoa(args.DiscoveryOptions.Port),
		Handler: discovery.RestContainer}

	addr := s.HTTPServer.Addr
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.HTTPListeningAddr = listener.Addr()

	grpcListener, err := net.Listen("tcp", args.DiscoveryOptions.GrpcAddr)
	if err != nil {
		return err
	}
	s.GRPCListeningAddr = grpcListener.Addr()

	s.addStartFunc(func(stop chan struct{}) error {
		log.Infof("Discovery service started at http=%s grpc=%s", listener.Addr().String(), grpcListener.Addr().String())

		go func() {
			if err = s.HTTPServer.Serve(listener); err != nil {
				log.Warna(err)
			}
		}()
		go func() {
			if err = s.EnvoyXdsServer.GrpcServer.Serve(grpcListener); err != nil {
				log.Warna(err)
			}
		}()

		go func() {
			<-stop
			err = s.HTTPServer.Close()
			if err != nil {
				log.Warna(err)
			}
			s.EnvoyXdsServer.GrpcServer.Stop()
		}()

		if args.RDSv2 {
			log.Info("xDS: enabling RDS")
			cache := envoyv2.NewConfigCache(s.ServiceController, s.configController)
			cache.Register(s.GRPCServer)
			cache.RegisterInput(s.ServiceController, s.configController)
		}

		return err
	})

	return nil
}

// initAdmissionController creates and initializes the k8s admission controller if running in a k8s environment.
func (s *Server) initAdmissionController(args *PilotArgs) error {
	if s.kubeClient == nil {
		// Not running in a k8s environment - do nothing.
		return nil
	}

	// Create the arguments for the admission controller
	admissionArgs := admit.ControllerOptions{
		ExternalAdmissionWebhookName: args.Admission.ExternalAdmissionWebhookName,
		ServiceName:                  args.Admission.ServiceName,
		SecretName:                   args.Admission.SecretName,
		Port:                         args.Admission.Port,
		RegistrationDelay:            args.Admission.RegistrationDelay,
		Descriptor:                   configDescriptor,
		ServiceNamespace:             args.Namespace,
		DomainSuffix:                 args.Config.ControllerOptions.DomainSuffix,
		ValidateNamespaces: []string{
			args.Config.ControllerOptions.WatchedNamespace,
			args.Namespace,
		},
	}

	admissionController, err := admit.NewController(s.kubeClient, admissionArgs)
	if err != nil {
		return err
	}

	// Defer running the admission controller.
	s.addStartFunc(func(stop chan struct{}) error {
		go admissionController.Run(stop)
		return nil
	})
	return nil
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func (s *Server) initGrpcServer() {
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

	// get the grpc server wired up
	grpc.EnableTracing = true

	s.GRPCServer = grpc.NewServer(grpcOptions...)
}

func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}
