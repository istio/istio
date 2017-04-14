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
	"istio.io/manager/apiserver"
	"istio.io/manager/cmd"
	"istio.io/manager/cmd/version"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/proxy"
	"istio.io/manager/proxy/envoy"
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

	defaultIngressController bool
}

var (
	flags  args
	client *kube.Client
	mesh   *proxyconfig.ProxyMeshConfig

	rootCmd = &cobra.Command{
		Use:   "manager",
		Short: "Istio Manager",
		Long:  "Istio Manager provides management plane functionality to the Istio proxy mesh and Istio Mixer.",
		PersistentPreRunE: func(*cobra.Command, []string) (err error) {
			client, err = kube.NewClient(flags.kubeconfig, model.IstioConfig)
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
			mesh, err = cmd.GetMeshConfig(client.GetKubernetesClient(), flags.controllerOptions.Namespace, flags.meshConfig)
			if err != nil {
				return multierror.Prefix(err, "failed to retrieve mesh configuration.")
			}

			glog.V(2).Infof("mesh configuration %s", spew.Sdump(mesh))
			return
		},
	}

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio Manager discovery service",
		RunE: func(c *cobra.Command, args []string) (err error) {
			controller := kube.NewController(client, flags.controllerOptions)
			context := &proxy.Context{
				Discovery:  controller,
				Accounts:   controller,
				Config:     &model.IstioRegistry{ConfigRegistry: controller},
				MeshConfig: mesh,
			}
			discovery, err := envoy.NewDiscoveryService(controller, context, flags.discoveryOptions)
			if err != nil {
				return fmt.Errorf("failed to create discovery service: %v", err)
			}
			stop := make(chan struct{})
			go controller.Run(stop)
			go discovery.Run()
			cmd.WaitSignal(stop)
			return
		},
	}

	apiserverCmd = &cobra.Command{
		Use:   "apiserver",
		Short: "Start Istio Manager config API service",
		Run: func(*cobra.Command, []string) {
			controller := kube.NewController(client, flags.controllerOptions)
			apiserver := apiserver.NewAPI(apiserver.APIServiceOptions{
				Version:  "v1alpha1",
				Port:     flags.apiserverPort,
				Registry: &model.IstioRegistry{ConfigRegistry: controller},
			})
			stop := make(chan struct{})
			go apiserver.Run()
			cmd.WaitSignal(stop)
		},
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Istio Proxy agent",
	}

	sidecarCmd = &cobra.Command{
		Use:   "sidecar",
		Short: "Istio Proxy sidecar agent",
		RunE: func(c *cobra.Command, args []string) (err error) {
			controller := kube.NewController(client, flags.controllerOptions)
			context := &proxy.Context{
				Discovery:        controller,
				Accounts:         controller,
				Config:           &model.IstioRegistry{ConfigRegistry: controller},
				MeshConfig:       mesh,
				IPAddress:        flags.ipAddress,
				UID:              fmt.Sprintf("kubernetes://%s.%s", flags.podName, flags.controllerOptions.Namespace),
				PassthroughPorts: flags.passthrough,
			}
			w, err := envoy.NewWatcher(controller, context)
			if err != nil {
				return
			}
			stop := make(chan struct{})
			go w.Run(stop)
			cmd.WaitSignal(stop)
			return
		},
	}

	ingressCmd = &cobra.Command{
		Use:   "ingress",
		Short: "Istio Proxy ingress controller",
		RunE: func(c *cobra.Command, args []string) error {
			flags.controllerOptions.IngressSyncMode = kube.IngressStrict
			controller := kube.NewController(client, flags.controllerOptions)
			config := &envoy.IngressConfig{
				CertFile:  "/etc/tls.crt",
				KeyFile:   "/etc/tls.key",
				Namespace: flags.controllerOptions.Namespace,
				Secrets:   controller,
				Registry:  &model.IstioRegistry{ConfigRegistry: controller},
				Mesh:      mesh,
				Port:      80,
				SSLPort:   443,
			}
			w, err := envoy.NewIngressWatcher(controller, config)
			if err != nil {
				return err
			}
			stop := make(chan struct{})
			go w.Run(stop)
			cmd.WaitSignal(stop)
			return nil
		},
	}

	egressCmd = &cobra.Command{
		Use:   "egress",
		Short: "Istio Proxy external service agent",
		RunE: func(c *cobra.Command, args []string) error {
			flags.controllerOptions.IngressSyncMode = kube.IngressOff
			controller := kube.NewController(client, flags.controllerOptions)
			config := &envoy.EgressConfig{
				Namespace: flags.controllerOptions.Namespace,
				Mesh:      mesh,
				Services:  controller,
				Port:      80,
			}
			w, err := envoy.NewEgressWatcher(controller, config)
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
	rootCmd.PersistentFlags().StringVar(&flags.meshConfig, "meshConfig", cmd.DefaultConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", cmd.ConfigMapKey))

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
	ingressCmd.PersistentFlags().StringVar(&flags.controllerOptions.IngressClass, "ingress_class", "istio",
		"The class of ingress resources to be processed by this ingress controller")
	ingressCmd.PersistentFlags().BoolVar(&flags.defaultIngressController, "default_ingress_controller", true,
		"Specifies whether running as the cluster's default ingress controller, "+
			"thereby processing unclassified ingress resources")

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
