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
	"istio.io/manager/cmd"
	"istio.io/manager/cmd/version"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/proxy/envoy"
)

type args struct {
	kubeconfig   string
	namespace    string
	resyncPeriod time.Duration
	config       string

	identity                 envoy.ProxyNode
	sdsPort                  int
	ingressSecret            string
	ingressClass             string
	defaultIngressController bool
	enableProfiling          bool
}

var (
	flags  = &args{}
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
			mesh, err = cmd.GetMeshConfig(client.GetKubernetesClient(), flags.namespace, flags.config)
			if err != nil {
				return multierror.Prefix(err, "failed to set mesh configuration.")
			}
			glog.V(2).Infof("mesh configuration %s", spew.Sdump(mesh))
			return
		},
	}

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio Manager discovery service",
		RunE: func(c *cobra.Command, args []string) (err error) {
			controller := kube.NewController(client, kube.ControllerConfig{
				Namespace:       flags.namespace,
				ResyncPeriod:    flags.resyncPeriod,
				IngressSyncMode: kube.IngressOff,
			})
			options := envoy.DiscoveryServiceOptions{
				Services: controller,
				Config: &model.IstioRegistry{
					ConfigRegistry: controller,
				},
				Mesh:            mesh,
				Port:            flags.sdsPort,
				EnableProfiling: flags.enableProfiling,
			}
			sds := envoy.NewDiscoveryService(options)
			stop := make(chan struct{})
			go controller.Run(stop)
			go sds.Run()
			cmd.WaitSignal(stop)
			return
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
			setFlagsFromEnv()
			controller := kube.NewController(client, kube.ControllerConfig{
				Namespace:       flags.namespace,
				ResyncPeriod:    flags.resyncPeriod,
				IngressSyncMode: kube.IngressOff,
			})
			w, err := envoy.NewWatcher(controller,
				controller,
				&model.IstioRegistry{ConfigRegistry: controller},
				mesh,
				&flags.identity)
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
			controllerConfig := kube.ControllerConfig{
				Namespace:       flags.namespace,
				ResyncPeriod:    flags.resyncPeriod,
				IngressSyncMode: kube.IngressStrict,
				IngressClass:    flags.ingressClass,
			}
			controller := kube.NewController(client, controllerConfig)
			config := &envoy.IngressConfig{
				CertFile:  "/etc/tls.crt",
				KeyFile:   "/etc/tls.key",
				Namespace: flags.namespace,
				Secret:    flags.ingressSecret,
				Secrets:   client,
				Registry:  &model.IstioRegistry{ConfigRegistry: controller},
				Mesh:      mesh,
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
			// TODO: implement this method
			stop := make(chan struct{})
			cmd.WaitSignal(stop)
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVarP(&flags.namespace, "namespace", "n", "",
		"Select a Kubernetes namespace for the controller loop ('' means all namespaces)")
	rootCmd.PersistentFlags().DurationVar(&flags.resyncPeriod, "resync", 100*time.Millisecond,
		"Controller resync interval")
	rootCmd.PersistentFlags().StringVar(&flags.config, "meshConfig", "istio",
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", cmd.ConfigMapKey))

	discoveryCmd.PersistentFlags().IntVarP(&flags.sdsPort, "port", "p", 8080,
		"Discovery service port")
	discoveryCmd.PersistentFlags().BoolVar(&flags.enableProfiling, "profile", true,
		"Enable profiling via web interface host:port/debug/pprof")

	proxyCmd.PersistentFlags().StringVar(&flags.identity.IP, "nodeIP", "",
		"Proxy node IP address. If not provided uses ${POD_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&flags.identity.Name, "nodeName", "",
		"Proxy node name. If not provided uses ${POD_NAME}.${POD_NAMESPACE}")

	// TODO: remove this once we write the logic to obtain secrets dynamically
	ingressCmd.PersistentFlags().StringVar(&flags.ingressSecret, "secret",
		"",
		"Kubernetes secret name for ingress SSL termination")
	ingressCmd.PersistentFlags().StringVar(&flags.ingressClass, "ingress_class",
		"istio",
		"The class of ingress resources to be processed by this ingress controller")
	ingressCmd.PersistentFlags().BoolVar(&flags.defaultIngressController, "default_ingress_controller",
		true,
		"Specifies whether running as the cluster's default ingress controller, "+
			"thereby processing unclassified ingress resources")

	proxyCmd.AddCommand(sidecarCmd)
	proxyCmd.AddCommand(ingressCmd)
	proxyCmd.AddCommand(egressCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(version.VersionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}

// setFlagsFromEnv sets default values for flags that are not specified
func setFlagsFromEnv() {
	if flags.identity.IP == "" {
		flags.identity.IP = os.Getenv("POD_IP")
	}
	if flags.identity.Name == "" {
		flags.identity.Name = fmt.Sprintf("%s.%s", os.Getenv("POD_NAME"), os.Getenv("POD_NAMESPACE"))
	}
}
