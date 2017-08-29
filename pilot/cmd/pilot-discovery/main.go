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

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/adapter/config/aggregate"
	"istio.io/pilot/adapter/config/crd"
	"istio.io/pilot/adapter/config/ingress"
	"istio.io/pilot/cmd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform"
	"istio.io/pilot/platform/consul"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/proxy/envoy"
	"istio.io/pilot/tools/version"
)

// ConsulArgs store the args related to Consul configuration
type ConsulArgs struct {
	config    string
	serverURL string
}

type args struct {
	kubeconfig string
	meshconfig string

	// ingress sync mode is set to off by default
	controllerOptions kube.ControllerOptions
	discoveryOptions  envoy.DiscoveryServiceOptions

	serviceregistry platform.ServiceRegistry
	consulargs      ConsulArgs
}

var (
	flags args

	rootCmd = &cobra.Command{
		Use:   "pilot",
		Short: "Istio Pilot",
		Long:  "Istio Pilot provides management plane functionality to the Istio service mesh and Istio Mixer.",
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

			var serviceController model.Controller
			var configController model.ConfigStoreCache
			environment := proxy.Environment{
				Mesh: mesh,
			}

			stop := make(chan struct{})

			// Set up values for input to discovery service in different platforms
			if flags.serviceregistry == platform.KubernetesRegistry || flags.serviceregistry == "" {

				client, err := kube.CreateInterface(flags.kubeconfig)
				if err != nil {
					return multierror.Prefix(err, "failed to connect to Kubernetes API.")
				}

				if flags.controllerOptions.Namespace == "" {
					flags.controllerOptions.Namespace = os.Getenv("POD_NAMESPACE")
				}

				glog.V(2).Infof("version %s", version.Line())
				glog.V(2).Infof("flags %s", spew.Sdump(flags))

				configClient, err := crd.NewClient(flags.kubeconfig, model.ConfigDescriptor{
					model.RouteRule,
					model.DestinationPolicy,
				})
				if err != nil {
					return multierror.Prefix(err, "failed to open a config client.")
				}

				if err = configClient.RegisterResources(); err != nil {
					return multierror.Prefix(err, "failed to register custom resources.")
				}

				kubeController := kube.NewController(client, mesh, flags.controllerOptions)
				if mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
					configController = crd.NewController(configClient, flags.controllerOptions)
				} else {
					configController, err = aggregate.MakeCache([]model.ConfigStoreCache{
						crd.NewController(configClient, flags.controllerOptions),
						ingress.NewController(client, mesh, flags.controllerOptions),
					})
					if err != nil {
						return err
					}
				}

				environment.ServiceDiscovery = kubeController
				environment.ServiceAccounts = kubeController
				environment.IstioConfigStore = model.MakeIstioStore(configController)
				environment.SecretRegistry = kube.MakeSecretRegistry(client)
				serviceController = kubeController
				ingressSyncer := ingress.NewStatusSyncer(mesh, client, flags.controllerOptions)

				go ingressSyncer.Run(stop)
			} else if flags.serviceregistry == platform.ConsulRegistry {
				glog.V(2).Infof("Consul url: %v", flags.consulargs.serverURL)

				consulController, err := consul.NewController(
					flags.consulargs.serverURL, "dc1", 2*time.Second)
				if err != nil {
					return fmt.Errorf("failed to create Consul controller: %v", err)
				}

				configClient, err := crd.NewClient(flags.kubeconfig, model.ConfigDescriptor{
					model.RouteRule,
					model.DestinationPolicy,
				})
				if err != nil {
					return multierror.Prefix(err, "failed to open a config client.")
				}

				if err = configClient.RegisterResources(); err != nil {
					return multierror.Prefix(err, "failed to register custom resources.")
				}

				configController = crd.NewController(configClient, flags.controllerOptions)

				environment.ServiceDiscovery = consulController
				environment.ServiceAccounts = consulController
				environment.IstioConfigStore = model.MakeIstioStore(configController)
				serviceController = consulController
			}

			// Set up discovery service
			discovery, err := envoy.NewDiscoveryService(
				serviceController,
				configController,
				environment,
				flags.discoveryOptions)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}

			go serviceController.Run(stop)
			go configController.Run(stop)
			go discovery.Run()
			cmd.WaitSignal(stop)
			return nil
		},
	}
)

func init() {
	discoveryCmd.PersistentFlags().StringVar((*string)(&flags.serviceregistry), "serviceregistry",
		string(platform.KubernetesRegistry),
		fmt.Sprintf("Select the platform for service registry, options are {%s, %s}",
			string(platform.KubernetesRegistry), string(platform.ConsulRegistry)))
	discoveryCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	discoveryCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshConfig", "/etc/istio/config/mesh",
		fmt.Sprintf("File name for Istio mesh configuration"))
	discoveryCmd.PersistentFlags().StringVarP(&flags.controllerOptions.Namespace, "namespace", "n", "",
		"Select a namespace for the controller loop. If not set, uses ${POD_NAMESPACE} environment variable")
	discoveryCmd.PersistentFlags().DurationVar(&flags.controllerOptions.ResyncPeriod, "resync", time.Second,
		"Controller resync interval")
	discoveryCmd.PersistentFlags().StringVar(&flags.controllerOptions.DomainSuffix, "domain", "cluster.local",
		"DNS domain suffix")

	discoveryCmd.PersistentFlags().IntVar(&flags.discoveryOptions.Port, "port", 8080,
		"Discovery service port")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableProfiling, "profile", true,
		"Enable profiling via web interface host:port/debug/pprof")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableCaching, "discovery_cache", true,
		"Enable caching discovery service responses")
	discoveryCmd.PersistentFlags().StringVar(&flags.consulargs.config, "consulconfig", "",
		"Consul Config file for discovery")
	discoveryCmd.PersistentFlags().StringVar(&flags.consulargs.serverURL, "consulserverURL", "",
		"URL for the consul server")

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
