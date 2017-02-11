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
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/proxy/envoy"

	multierror "github.com/hashicorp/go-multierror"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

type args struct {
	kubeconfig string
	namespace  string
	client     *kube.Client
	server     serverArgs
	proxy      envoy.MeshConfig
}

type serverArgs struct {
	sdsPort int
}

const (
	resyncPeriod = 100 * time.Millisecond
)

var (
	flags = &args{}

	rootCmd = &cobra.Command{
		Use:   "manager",
		Short: "Istio Manager",
		Long: `
Istio Manager provides management plane functionality to the Istio proxy mesh and Istio Mixer.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			glog.V(2).Infof("flags: %#v", flags)
			flags.client, err = kube.NewClient(flags.kubeconfig, model.IstioConfig)
			if err != nil {
				flags.client, err = kube.NewClient(os.Getenv("HOME")+"/.kube/config", model.IstioConfig)
				if err != nil {
					return multierror.Prefix(err, "Failed to connect to Kubernetes API.")
				}
			}
			if err = flags.client.RegisterResources(); err != nil {
				return multierror.Prefix(err, "Failed to register Third-Party Resources.")
			}
			return
		},
	}

	serverCmd = &cobra.Command{
		Use:   "server",
		Short: "Start Istio Manager service",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			controller := kube.NewController(flags.client, flags.namespace, resyncPeriod)
			sds := envoy.NewDiscoveryService(controller, flags.server.sdsPort)
			stop := make(chan struct{})
			go controller.Run(stop)
			go sds.Run()
			waitSignal(stop)
			return
		},
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Start Istio Proxy sidecar agent",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			controller := kube.NewController(flags.client, flags.namespace, resyncPeriod)
			_, err = envoy.NewWatcher(controller, controller, &model.IstioRegistry{ConfigRegistry: controller},
				&flags.proxy)
			if err != nil {
				return
			}
			stop := make(chan struct{})
			go controller.Run(stop)
			waitSignal(stop)
			return
		},
	}

	egressCmd = &cobra.Command{
		Use:   "egress",
		Short: "Start Istio Proxy external service agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement this method
			stop := make(chan struct{})
			waitSignal(stop)
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&flags.kubeconfig, "kubeconfig", "c", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVarP(&flags.namespace, "namespace", "n", "",
		"Select the specified namespace instead of all namespaces")
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	serverCmd.PersistentFlags().IntVarP(&flags.server.sdsPort, "port", "p", 8080,
		"Discovery service port")
	rootCmd.AddCommand(serverCmd)

	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.DiscoveryAddress, "sds", "s", "manager:8080",
		"Discovery service DNS address")
	proxyCmd.PersistentFlags().IntVarP(&flags.proxy.ProxyPort, "port", "p", 5001,
		"Envoy proxy port")
	proxyCmd.PersistentFlags().IntVarP(&flags.proxy.AdminPort, "admin_port", "a", 5000,
		"Envoy admin port")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.BinaryPath, "envoy_path", "b", "/usr/local/bin/envoy",
		"Envoy binary location")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.ConfigPath, "config_path", "e", "/etc/envoy",
		"Envoy config root location")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.MixerAddress, "mixer", "m", "",
		"Mixer DNS address (or empty to disable Mixer)")
	rootCmd.AddCommand(proxyCmd)
	proxyCmd.AddCommand(egressCmd)

	rootCmd.AddCommand(configCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Error(err)
		os.Exit(-1)
	}
}

func waitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	close(stop)
	glog.Flush()
}
