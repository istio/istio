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

package k8s

import (
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	meshv1 "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/kube/crd/controller"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/istiod"
	agent_cache "istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	"istio.io/pkg/log"

	controller2 "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
)

// Helpers to configure the k8s-dependent registries
// To reduce binary size/deps, the standalone hyperistio for VMs will try to not depend on k8s, keeping all
// init deps in this package.

type Controllers struct {
	IstioServer       *istiod.Server
	ControllerOptions controller2.Options

	kubeClient   kubernetes.Interface
	kubeCfg      *rest.Config
	kubeRegistry *controller2.Controller
	multicluster *clusterregistry.Multicluster
	args         *istiod.PilotArgs
}

func InitK8S(is *istiod.Server, clientset *kubernetes.Clientset, config *rest.Config, args *istiod.PilotArgs) (*Controllers, error) {
	s := &Controllers{
		IstioServer: is,
		kubeCfg:     config,
		kubeClient:  clientset,
		args:        args,
	}

	// Istio's own K8S config controller - shouldn't be needed if MCP is used.
	// TODO: ordering, this needs to go before discovery.
	if err := s.initConfigController(args); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}
	return s, nil
}

func (s *Controllers) OnXDSStart(xds model.XDSUpdater) {
	s.kubeRegistry.XDSUpdater = xds
}

func (s *Controllers) InitK8SDiscovery(is *istiod.Server, clientset *kubernetes.Clientset, config *rest.Config, args *istiod.PilotArgs) (*Controllers, error) {
	if err := s.createK8sServiceControllers(s.IstioServer.ServiceController); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}

	if err := s.initClusterRegistries(args); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}

	// kubeRegistry may use the environment for push status reporting.
	// TODO: maybe all registries should have this as an optional field ?
	s.kubeRegistry.Env = s.IstioServer.Environment
	s.kubeRegistry.InitNetworkLookup(s.IstioServer.MeshNetworks)
	// EnvoyXDSServer is not initialized yet - since initialization adds all 'service' handlers, which depends
	// on this being done. Instead we use the callback.
	//s.kubeRegistry.XDSUpdater = s.IstioServer.EnvoyXdsServer

	return s, nil
}

func (s *Controllers) WaitForCacheSync(stop <-chan struct{}) bool {
	if !cache.WaitForCacheSync(stop, func() bool {
		return !s.IstioServer.ConfigController.HasSynced()
	}) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}

	return true
}

// initClusterRegistries starts the secret controller to watch for remote
// clusters and initialize the multicluster structures.s.
func (s *Controllers) initClusterRegistries(args *istiod.PilotArgs) (err error) {

	mc, err := clusterregistry.NewMulticluster(s.kubeClient,
		args.Config.ClusterRegistriesNamespace,
		s.ControllerOptions.WatchedNamespace,
		args.DomainSuffix,
		s.ControllerOptions.ResyncPeriod,
		s.IstioServer.ServiceController,
		s.IstioServer.EnvoyXdsServer,
		s.IstioServer.MeshNetworks)

	if err != nil {
		log.Info("Unable to create new Multicluster object")
		return err
	}

	s.multicluster = mc
	return nil
}

// initConfigController creates the config controller in the pilotConfig.
func (s *Controllers) initConfigController(args *istiod.PilotArgs) error {
	cfgController, err := s.makeKubeConfigController(args)
	if err != nil {
		return err
	}

	s.IstioServer.ConfigStores = append(s.IstioServer.ConfigStores, cfgController)

	// Defer starting the controller until after the service is created.
	s.IstioServer.AddStartFunc(func(stop <-chan struct{}) error {
		go cfgController.Run(stop)
		return nil
	})

	// If running in ingress mode (requires k8s), wrap the config controller.
	if s.IstioServer.Mesh.IngressControllerMode != meshconfig.MeshConfig_OFF {
		s.IstioServer.ConfigStores = append(s.IstioServer.ConfigStores, ingress.NewController(s.kubeClient, s.IstioServer.Mesh, s.ControllerOptions))

		if ingressSyncer, errSyncer := ingress.NewStatusSyncer(s.IstioServer.Mesh, s.kubeClient,
			args.Namespace, s.ControllerOptions); errSyncer != nil {
			log.Warnf("Disabled ingress status syncer due to %v", errSyncer)
		} else {
			s.IstioServer.AddStartFunc(func(stop <-chan struct{}) error {
				go ingressSyncer.Run(stop)
				return nil
			})
		}
	}

	return nil
}

// createK8sServiceControllers creates all the k8s service controllers under this pilot
func (s *Controllers) createK8sServiceControllers(serviceControllers *aggregate.Controller) (err error) {
	clusterID := string(serviceregistry.KubernetesRegistry)
	log.Infof("Primary Cluster name: %s", clusterID)
	s.ControllerOptions.ClusterID = clusterID
	kubectl := controller2.NewController(s.kubeClient, s.ControllerOptions)
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

func (s *Controllers) makeKubeConfigController(args *istiod.PilotArgs) (model.ConfigStoreCache, error) {
	kubeCfgFile := args.Config.KubeConfig
	configClient, err := controller.NewClient(kubeCfgFile, "", schemas.Istio, s.ControllerOptions.DomainSuffix, &model.DisabledLedger{})
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if !args.Config.DisableInstallCRDs {
		if err = configClient.RegisterResources(); err != nil {
			return nil, multierror.Prefix(err, "failed to register custom resources.")
		}
	}

	return controller.NewController(configClient, s.ControllerOptions), nil
}

const (
	// ConfigMapKey should match the expected MeshConfig file name
	ConfigMapKey = "mesh"
)

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

type testHandler struct {
}

func (t testHandler) Handle(e event.Event) {
	log.Debugf("Event %v", e)
}

func (s *Controllers) NewGalleyK8SSource(resources schema.KubeResources) (src event.Source, err error) {

	o := apiserver.Options{
		Client:       kube.NewInterfaces(s.kubeCfg),
		ResyncPeriod: s.ControllerOptions.ResyncPeriod,
		Resources:    resources,
	}
	src = apiserver.New(o)

	src.Dispatch(testHandler{})

	return
}

// Start the workload SDS server. Will run on the UDS path - Envoy sidecar will use a cluster
// to expose the UDS path over TLS, using Apiserver-signed certs.
// SDS depends on k8s.
//
// TODO: modify NewSecretFetcher, add method taking a kube client ( for consistency )
// TODO: modify NewServer, add method taking a grpcServer
func (s *Controllers) StartSDSK8S(config *meshv1.MeshConfig) error {

	// This won't work on VM - only on K8S.
	var sdsCacheOptions agent_cache.Options
	var serverOptions sds.Options

	// Compat with Istio env - will determine the plugin used for connecting to the CA.
	caProvider := os.Getenv("CA_PROVIDER")
	if caProvider == "" {
		caProvider = "Citadel"
	}

	// Compat with Istio env
	// Will use istio-system/istio-security config map to load the root CA of citadel.
	// The address can be the Gateway address for the cluster
	// TODO: load the GW address of the cluster to auto-configure
	// TODO: Citadel should also use k8s-api signed certificates ( possibly on different port )
	caAddr := os.Getenv("CA_ADDR")
	if caAddr == "" {
		// caAddr = "istio-citadel.istio-system:8060"
		// For testing with port fwd (kfwd istio-system istio=citadel 8060:8060)
		caAddr = "localhost:8060"
	}

	wSecretFetcher, err := secretfetcher.NewSecretFetcher(false,
		caAddr, caProvider, true,
		[]byte(""), "", "", "", "")
	if err != nil {
		log.Fatalf("failed to create secretFetcher for workload proxy %v", err)
	}
	sdsCacheOptions.TrustDomain = serverOptions.TrustDomain
	sdsCacheOptions.RotationInterval = 5 * time.Minute
	serverOptions.RecycleInterval = 5 * time.Minute
	serverOptions.EnableWorkloadSDS = true
	serverOptions.WorkloadUDSPath = "./sdsUDS"
	serverOptions.GrpcServer = s.IstioServer.SecureGRPCServer

	sdsCacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
	workloadSecretCache := agent_cache.NewSecretCache(wSecretFetcher, sds.NotifyProxy, sdsCacheOptions)

	// GatewaySecretCache loads secrets from K8S - they should be in same namespace with ingress gateway, will use
	// standalone node agent.
	_, err = sds.NewServer(serverOptions, workloadSecretCache, nil)

	if err != nil {
		log.Fatalf("Failed to start SDS server %v", err)
	}

	return nil
}
