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
	"istio.io/pilot/adapter/config/tpr"
	"istio.io/pilot/apiserver"
	"istio.io/pilot/cmd"
	"istio.io/pilot/cmd/version"
	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/proxy/envoy"
)

type args struct {
	kubeconfig string
	meshConfig string

	ipAddress     string
	podName       string
	passthrough   []int
	apiserverPort int

	// ingress sync mode is set to off by default
	controllerOptions kube.ControllerOptions
	discoveryOptions  envoy.DiscoveryServiceOptions
}

var (
	flags  args
	client *tpr.Client
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

			client, err = tpr.NewClient(flags.kubeconfig, model.IstioConfigTypes, flags.controllerOptions.Namespace)
			if err != nil {
				return multierror.Prefix(err, "failed to connect to Kubernetes API.")
			}
			if err = client.RegisterResources(); err != nil {
				return multierror.Prefix(err, "failed to register Third-Party Resources.")
			}

			// set values from environment variables
			if flags.ipAddress == "" {
				flags.ipAddress = os.Getenv("POD_IP")
			}
			if flags.podName == "" {
				flags.podName = os.Getenv("POD_NAME")
			}
			if flags.controllerOptions.Namespace == "" {
				flags.controllerOptions.Namespace = os.Getenv("POD_NAMESPACE")
			}
			glog.V(2).Infof("flags %s", spew.Sdump(flags))

			// receive mesh configuration
			mesh, err = cmd.GetMeshConfig(client.GetKubernetesInterface(), flags.controllerOptions.Namespace, flags.meshConfig)
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
		RunE: func(c *cobra.Command, args []string) (err error) {
			serviceController := kube.NewController(client.GetKubernetesInterface(), mesh, flags.controllerOptions)
			configController := tpr.NewController(client, mesh, flags.controllerOptions)
			context := &proxy.Context{
				Discovery:  serviceController,
				Accounts:   serviceController,
				Config:     model.MakeIstioStore(configController),
				MeshConfig: mesh,
			}
			discovery, err := envoy.NewDiscoveryService(serviceController, configController, context, flags.discoveryOptions)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}
			stop := make(chan struct{})
			go serviceController.Run(stop)
			go configController.Run(stop)
			go discovery.Run()
			cmd.WaitSignal(stop)
			return
		},
	}

	apiserverCmd = &cobra.Command{
		Use:   "apiserver",
		Short: "Start Istio config API service",
		Run: func(*cobra.Command, []string) {
			controller := tpr.NewController(client, mesh, flags.controllerOptions)
			apiserver := apiserver.NewAPI(apiserver.APIServiceOptions{
				Version:  tpr.IstioResourceVersion,
				Port:     flags.apiserverPort,
				Registry: controller,
			})
			stop := make(chan struct{})
			go controller.Run(stop)
			go apiserver.Run()
			cmd.WaitSignal(stop)
		},
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Envoy agent",
	}

	sidecarCmd = &cobra.Command{
		Use:   "sidecar",
		Short: "Envoy sidecar agent",
		RunE: func(c *cobra.Command, args []string) (err error) {
			mesh.IngressControllerMode = proxyconfig.ProxyMeshConfig_OFF
			serviceController := kube.NewController(client.GetKubernetesInterface(), mesh, flags.controllerOptions)
			configController := tpr.NewController(client, mesh, flags.controllerOptions)
			context := &proxy.Context{
				Discovery:        serviceController,
				Accounts:         serviceController,
				Config:           model.MakeIstioStore(configController),
				MeshConfig:       mesh,
				IPAddress:        flags.ipAddress,
				UID:              fmt.Sprintf("kubernetes://%s.%s", flags.podName, flags.controllerOptions.Namespace),
				PassthroughPorts: flags.passthrough,
			}
			w, err := envoy.NewWatcher(serviceController, configController, context)
			if err != nil {
				return
			}
			stop := make(chan struct{})

			// must start watcher after starting dependent controllers
			go serviceController.Run(stop)
			go configController.Run(stop)
			go w.Run(stop)

			cmd.WaitSignal(stop)
			return
		},
	}

	ingressCmd = &cobra.Command{
		Use:   "ingress",
		Short: "Envoy ingress agent",
		RunE: func(c *cobra.Command, args []string) error {
			s := kube.NewIngressStatusSyncer(mesh, client.GetKubernetesInterface(), flags.controllerOptions)
			w, err := envoy.NewIngressWatcher(mesh, kube.MakeSecretRegistry(client.GetKubernetesInterface()))
			if err != nil {
				return err
			}
			stop := make(chan struct{})
			go w.Run(stop)
			go s.Run(stop)
			cmd.WaitSignal(stop)
			return nil
		},
	}

	egressCmd = &cobra.Command{
		Use:   "egress",
		Short: "Envoy external service agent",
		RunE: func(c *cobra.Command, args []string) error {
			w, err := envoy.NewEgressWatcher(mesh)
			if err != nil {
				return err
			}
			stop := make(chan struct{})
			go w.Run(stop)
			cmd.WaitSignal(stop)
			return nil
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

	apiserverCmd.PersistentFlags().IntVar(&flags.apiserverPort, "port", 8081,
		"Config API service port")

	proxyCmd.PersistentFlags().StringVar(&flags.ipAddress, "ipAddress", "",
		"IP address. If not provided uses ${POD_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&flags.podName, "podName", "",
		"Pod name. If not provided uses ${POD_NAME} environment variable")

	sidecarCmd.PersistentFlags().IntSliceVar(&flags.passthrough, "passthrough", nil,
		"Passthrough ports for health checks")

	proxyCmd.AddCommand(sidecarCmd)
	proxyCmd.AddCommand(ingressCmd)
	proxyCmd.AddCommand(egressCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(apiserverCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(version.VersionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}
