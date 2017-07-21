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
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/adapter/config/aggregate"
	"istio.io/pilot/adapter/config/ingress"
	"istio.io/pilot/adapter/config/tpr"
	"istio.io/pilot/cmd"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/proxy/envoy"
	"istio.io/pilot/tools/version"
)

type args struct {
	kubeconfig string
	meshConfig string

	sidecar proxy.Sidecar

	// ingress sync mode is set to off by default
	controllerOptions kube.ControllerOptions
	discoveryOptions  envoy.DiscoveryServiceOptions
}

var (
	flags  args
	client kubernetes.Interface
	mesh   *proxyconfig.ProxyMeshConfig

	rootCmd = &cobra.Command{
		Use:   "pilot",
		Short: "Istio Pilot",
		Long:  "Istio Pilot provides management plane functionality to the Istio service mesh and Istio Mixer.",
		PersistentPreRunE: func(*cobra.Command, []string) (err error) {
			if flags.kubeconfig == "" {
				if v := os.Getenv("KUBECONFIG"); v != "" {
					glog.V(2).Infof("Setting configuration from KUBECONFIG environment variable")
					flags.kubeconfig = v
				}
			}

			client, err = kube.CreateInterface(flags.kubeconfig)
			if err != nil {
				return multierror.Prefix(err, "failed to connect to Kubernetes API.")
			}

			// set values from environment variables
			if flags.sidecar.IPAddress == "" {
				flags.sidecar.IPAddress = os.Getenv("POD_IP")
			}
			if flags.sidecar.InstanceName == "" {
				flags.sidecar.InstanceName = os.Getenv("POD_NAME")
			}
			if flags.sidecar.InstanceNamespace == "" {
				flags.sidecar.InstanceNamespace = os.Getenv("POD_NAMESPACE")
			}

			if flags.controllerOptions.Namespace == "" {
				flags.controllerOptions.Namespace = os.Getenv("POD_NAMESPACE")
			}
			glog.V(2).Infof("version %s", version.Line())
			glog.V(2).Infof("flags %s", spew.Sdump(flags))

			// receive mesh configuration
			mesh, err = cmd.GetMeshConfig(client, flags.controllerOptions.Namespace, flags.meshConfig)
			if err != nil {
				return multierror.Prefix(err, "failed to retrieve mesh configuration.")
			}

			glog.V(2).Infof("mesh configuration %s", spew.Sdump(mesh))
			return
		},
	}

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio proxy discovery service",
		RunE: func(c *cobra.Command, args []string) error {
			tprClient, err := tpr.NewClient(flags.kubeconfig, model.ConfigDescriptor{
				model.RouteRuleDescriptor,
				model.DestinationPolicyDescriptor,
			}, flags.controllerOptions.Namespace)
			if err != nil {
				return multierror.Prefix(err, "failed to open a TPR client")
			}

			if err = tprClient.RegisterResources(); err != nil {
				return multierror.Prefix(err, "failed to register Third-Party Resources.")
			}

			serviceController := kube.NewController(client, mesh, flags.controllerOptions)
			var configController model.ConfigStoreCache
			if mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
				configController = tpr.NewController(tprClient, flags.controllerOptions.ResyncPeriod)
			} else {
				configController, err = aggregate.MakeCache([]model.ConfigStoreCache{
					tpr.NewController(tprClient, flags.controllerOptions.ResyncPeriod),
					ingress.NewController(client, mesh, flags.controllerOptions),
				})
				if err != nil {
					return err
				}
			}

			environment := proxy.Environment{
				ServiceDiscovery: serviceController,
				ServiceAccounts:  serviceController,
				IstioConfigStore: model.MakeIstioStore(configController),
				Mesh:             mesh,
			}
			discovery, err := envoy.NewDiscoveryService(serviceController, configController, environment, flags.discoveryOptions)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}

			ingressSyncer := ingress.NewStatusSyncer(mesh, client, flags.controllerOptions)

			stop := make(chan struct{})
			go serviceController.Run(stop)
			go configController.Run(stop)
			go discovery.Run()
			go ingressSyncer.Run(stop)
			cmd.WaitSignal(stop)

			return nil
		},
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Envoy proxy agent",
		RunE: func(c *cobra.Command, args []string) error {
			var role proxy.Role = flags.sidecar
			if len(args) > 0 {
				switch args[0] {
				case proxy.EgressNode:
					if mesh.EgressProxyAddress == "" {
						return errors.New("egress proxy requires address configuration")
					}
					role = proxy.EgressRole{}

				case proxy.IngressNode:
					if mesh.IngressControllerMode == proxyconfig.ProxyMeshConfig_OFF {
						return errors.New("ingress proxy is disabled")
					}
					role = proxy.IngressRole{}

				default:
					return fmt.Errorf("failed to recognize proxy role %s", args[0])
				}
			}

			watcher := envoy.NewWatcher(mesh, role, kube.MakeSecretRegistry(client))
			ctx, cancel := context.WithCancel(context.Background())
			go watcher.Run(ctx)

			stop := make(chan struct{})
			cmd.WaitSignal(stop)
			<-stop
			cancel()
			return nil
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version information and exit",
		Run: func(*cobra.Command, []string) {
			fmt.Print(version.Version())
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVarP(&flags.controllerOptions.Namespace, "namespace", "n", "",
		"Select a namespace for the controller loop. If not set, uses ${POD_NAMESPACE} environment variable")
	rootCmd.PersistentFlags().DurationVar(&flags.controllerOptions.ResyncPeriod, "resync", time.Second,
		"Controller resync interval")
	rootCmd.PersistentFlags().StringVar(&flags.controllerOptions.DomainSuffix, "domainSuffix", "cluster.local",
		"Kubernetes DNS domain suffix")
	rootCmd.PersistentFlags().StringVar(&flags.meshConfig, "meshConfig", cmd.DefaultConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, config key should be %q", cmd.ConfigMapKey))

	discoveryCmd.PersistentFlags().IntVar(&flags.discoveryOptions.Port, "port", 8080,
		"Discovery service port")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableProfiling, "profile", true,
		"Enable profiling via web interface host:port/debug/pprof")
	discoveryCmd.PersistentFlags().BoolVar(&flags.discoveryOptions.EnableCaching, "discovery_cache", true,
		"Enable caching discovery service responses")

	proxyCmd.PersistentFlags().StringVar(&flags.sidecar.IPAddress, "ipAddress", "",
		"Sidecar IP address. If not provided uses ${POD_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&flags.sidecar.InstanceName, "podName", "",
		"Sidecar pod name. If not provided uses ${POD_NAME} environment variable")
	proxyCmd.PersistentFlags().StringVar(&flags.sidecar.InstanceNamespace, "podNamespace", "",
		"Sidecar pod namespace. If not provided uses ${POD_NAMESPACE} environment variable")

	//sidecarCmd.PersistentFlags().IntSliceVar(&flags.passthrough, "passthrough", nil,
	//	"Passthrough ports for health checks")

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}
