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

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	proxyconfig "istio.io/api/proxy/v1/config"
	configaggregate "istio.io/pilot/adapter/config/aggregate"
	"istio.io/pilot/adapter/config/crd"
	"istio.io/pilot/adapter/config/ingress"
	"istio.io/pilot/adapter/serviceregistry/aggregate"
	"istio.io/pilot/cmd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform"
	"istio.io/pilot/platform/consul"
	"istio.io/pilot/platform/eureka"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/platform/kube/admit"
	"istio.io/pilot/proxy"
	"istio.io/pilot/proxy/envoy"
	"istio.io/pilot/tools/version"
)

type consulArgs struct {
	config    string
	serverURL string
}

type eurekaArgs struct {
	serverURL string
}

type args struct {
	kubeconfig string
	meshconfig string

	// namespace for the controller (typically istio installation namespace)
	namespace string

	// ingress sync mode is set to off by default
	controllerOptions kube.ControllerOptions
	discoveryOptions  envoy.DiscoveryServiceOptions

	registries    []string
	consul        consulArgs
	eureka        eurekaArgs
	admissionArgs admit.ControllerOptions
}

var (
	flags args

	rootCmd = &cobra.Command{
		Use:   "pilot",
		Short: "Istio Pilot",
		Long:  "Istio Pilot provides fleet-wide traffic management capabilities in the Istio Service Mesh.",
	}

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio proxy discovery service",
		RunE: func(c *cobra.Command, args []string) error {

			// receive mesh configuration
			mesh, fail := cmd.ReadMeshConfig(flags.meshconfig)
			if fail != nil {
				defaultMesh := proxy.DefaultMeshConfig()
				mesh = &defaultMesh
				glog.Warningf("failed to read mesh configuration, using default: %v", fail)
			}

			glog.V(2).Infof("mesh configuration %s", spew.Sdump(mesh))
			glog.V(2).Infof("version %s", version.Line())
			glog.V(2).Infof("flags %s", spew.Sdump(flags))

			stop := make(chan struct{})

			if flags.namespace == "" {
				flags.namespace = os.Getenv("POD_NAMESPACE")
			}

			_, client, kuberr := kube.CreateInterface(flags.kubeconfig)
			if kuberr != nil {
				return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
			}

			configClient, err := crd.NewClient(flags.kubeconfig, model.ConfigDescriptor{
				model.RouteRule,
				model.EgressRule,
				model.DestinationPolicy,
			}, flags.controllerOptions.DomainSuffix)
			if err != nil {
				return multierror.Prefix(err, "failed to open a config client.")
			}

			if err = configClient.RegisterResources(); err != nil {
				return multierror.Prefix(err, "failed to register custom resources.")
			}

			configController := crd.NewController(configClient, flags.controllerOptions)
			serviceControllers := aggregate.NewController()
			registered := make(map[platform.ServiceRegistry]bool)
			for _, r := range flags.registries {
				serviceRegistry := platform.ServiceRegistry(r)
				if _, exists := registered[serviceRegistry]; exists {
					return multierror.Prefix(err, r+" registry specified multiple times.")
				}
				registered[serviceRegistry] = true
				glog.V(2).Infof("Adding %s registry adapter", serviceRegistry)
				switch serviceRegistry {
				case platform.KubernetesRegistry:
					kubectl := kube.NewController(client, flags.controllerOptions)
					serviceControllers.AddRegistry(
						aggregate.Registry{
							Name:             serviceRegistry,
							ServiceDiscovery: kubectl,
							ServiceAccounts:  kubectl,
							Controller:       kubectl,
						})
					if mesh.IngressControllerMode != proxyconfig.MeshConfig_OFF {
						configController, err = configaggregate.MakeCache([]model.ConfigStoreCache{
							configController,
							ingress.NewController(client, mesh, flags.controllerOptions),
						})
						if err != nil {
							return err
						}
					}

					if ingressSyncer, errSyncer := ingress.NewStatusSyncer(mesh, client,
						flags.namespace, flags.controllerOptions); errSyncer != nil {
						glog.Warningf("Disabled ingress status syncer due to %v", errSyncer)
					} else {
						go ingressSyncer.Run(stop)
					}

				case platform.ConsulRegistry:
					glog.V(2).Infof("Consul url: %v", flags.consul.serverURL)
					conctl, conerr := consul.NewController(
						// TODO: Remove this hardcoding!
						flags.consul.serverURL, "dc1", 2*time.Second)
					if conerr != nil {
						return fmt.Errorf("failed to create Consul controller: %v", conerr)
					}

					serviceControllers.AddRegistry(
						aggregate.Registry{
							Name:             serviceRegistry,
							ServiceDiscovery: conctl,
							ServiceAccounts:  conctl,
							Controller:       conctl,
						})
				case platform.EurekaRegistry:
					glog.V(2).Infof("Eureka url: %v", flags.eureka.serverURL)
					client := eureka.NewClient(flags.eureka.serverURL)
					serviceControllers.AddRegistry(
						aggregate.Registry{
							Name: serviceRegistry,
							// TODO: Remove sync time hardcoding!
							Controller:       eureka.NewController(client, 2*time.Second),
							ServiceDiscovery: eureka.NewServiceDiscovery(client),
							ServiceAccounts:  eureka.NewServiceAccounts(),
						})
				default:
					return multierror.Prefix(err, "Service registry "+r+" is not supported.")
				}
			}

			environment := proxy.Environment{
				Mesh:             mesh,
				IstioConfigStore: model.MakeIstioStore(configController),
				ServiceDiscovery: serviceControllers,
				ServiceAccounts:  serviceControllers,
			}

			// Set up discovery service
			discovery, err := envoy.NewDiscoveryService(
				serviceControllers,
				configController,
				environment,
				flags.discoveryOptions)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}

			// Set up configuration validation admission
			// controller. Fill in remaining admission controller
			// options
			flags.admissionArgs.Descriptor = configClient.ConfigDescriptor()
			flags.admissionArgs.ServiceNamespace = flags.namespace
			flags.admissionArgs.DomainSuffix = flags.controllerOptions.DomainSuffix
			flags.admissionArgs.ValidateNamespaces = []string{
				flags.controllerOptions.WatchedNamespace,
			}
			admissionController, err := admit.NewController(client, flags.admissionArgs)
			if err != nil {
				return fmt.Errorf("failed to create validation admission controller: %v", err)
			}

			go admissionController.Run(stop)
			go serviceControllers.Run(stop)
			go configController.Run(stop)
			go discovery.Run()
			cmd.WaitSignal(stop)
			return nil
		},
	}
)

func init() {
	discoveryCmd.PersistentFlags().StringSliceVar(&flags.registries, "registries",
		[]string{string(platform.KubernetesRegistry)},
		fmt.Sprintf("Comma separated list of platform service registries to read from (choose one or more from {%s, %s, %s})",
			platform.KubernetesRegistry, platform.ConsulRegistry, platform.EurekaRegistry))
	discoveryCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	discoveryCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshConfig", "/etc/istio/config/mesh",
		fmt.Sprintf("File name for Istio mesh configuration"))
	discoveryCmd.PersistentFlags().StringVarP(&flags.namespace, "namespace", "n", "",
		"Select a namespace where the controller resides. If not set, uses ${POD_NAMESPACE} environment variable")
	discoveryCmd.PersistentFlags().StringVarP(&flags.controllerOptions.WatchedNamespace, "appNamespace",
		"a", metav1.NamespaceAll,
		"Restrict the applications namespace the controller manages; if not set, controller watches all namespaces")
	discoveryCmd.PersistentFlags().DurationVar(&flags.controllerOptions.ResyncPeriod, "resync", 60*time.Second,
		"Controller resync interval")
	discoveryCmd.PersistentFlags().StringVar(&flags.controllerOptions.DomainSuffix, "domain", "cluster.local",
		"DNS domain suffix")

	discoveryCmd.PersistentFlags().IntVar(&flags.discoveryOptions.Port, "port", 8080,
		"Discovery service port")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableProfiling, "profile", true,
		"Enable profiling via web interface host:port/debug/pprof")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableCaching, "discovery_cache", true,
		"Enable caching discovery service responses")

	discoveryCmd.PersistentFlags().StringVar(&flags.consul.config, "consulconfig", "",
		"Consul Config file for discovery")
	discoveryCmd.PersistentFlags().StringVar(&flags.consul.serverURL, "consulserverURL", "",
		"URL for the Consul server")
	discoveryCmd.PersistentFlags().StringVar(&flags.eureka.serverURL, "eurekaserverURL", "",
		"URL for the Eureka server")

	discoveryCmd.PersistentFlags().StringVar(&flags.admissionArgs.ExternalAdmissionWebhookName,
		"admission-webhook-name", "pilot-webhook.istio.io", "Webhook name for Pilot admission controller")
	discoveryCmd.PersistentFlags().StringVar(&flags.admissionArgs.ServiceName,
		"admission-service", "istio-pilot-external",
		"Service name the admission controller uses during registration")
	discoveryCmd.PersistentFlags().IntVar(&flags.admissionArgs.Port, "admission-service-port", 443,
		"HTTPS port of the admission service. Must be 443 if service has more than one port ")
	discoveryCmd.PersistentFlags().StringVar(&flags.admissionArgs.SecretName, "admission-secret", "pilot-webhook",
		"Name of k8s secret for pilot webhook certs")
	discoveryCmd.PersistentFlags().DurationVar(&flags.admissionArgs.RegistrationDelay,
		"admission-registration-delay", 5*time.Second,
		"Time to delay webhook registration after starting webhook server")

	cmd.AddFlags(rootCmd)
	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(cmd.VersionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}
