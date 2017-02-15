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
	"fmt"
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
	identity   envoy.ProxyNode
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

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio Manager discovery service",
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
		Short: "Istio Proxy agent",
	}

	sidecarCmd = &cobra.Command{
		Use:   "sidecar",
		Short: "Istio Proxy sidecar agent",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			setFlagsFromEnv()
			controller := kube.NewController(flags.client, flags.namespace, resyncPeriod)
			_, err = envoy.NewWatcher(controller, controller, &model.IstioRegistry{ConfigRegistry: controller},
				&flags.proxy, &flags.identity)
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
		Short: "Istio Proxy external service agent",
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
		"Select the specified namespace for the Kubernetes controller to watch instead of all namespaces")
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	discoveryCmd.PersistentFlags().IntVarP(&flags.server.sdsPort, "port", "p", 8080,
		"Discovery service port")

	proxyCmd.PersistentFlags().StringVar(&flags.identity.IP, "nodeIP", "",
		"Proxy node IP address. If not provided uses ${POD_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&flags.identity.Name, "nodeName", "",
		"Proxy node name. If not provided uses ${POD_NAME}.${POD_NAMESPACE}")
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

	rootCmd.AddCommand(discoveryCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(proxyCmd)
	proxyCmd.AddCommand(sidecarCmd)
	proxyCmd.AddCommand(egressCmd)
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

// setFlagsFromEnv sets default values for flags that are not specified
func setFlagsFromEnv() {
	if flags.identity.IP == "" {
		flags.identity.IP = os.Getenv("POD_IP")
	}
	if flags.identity.Name == "" {
		flags.identity.Name = fmt.Sprintf("%s.%s", os.Getenv("POD_NAME"), os.Getenv("POD_NAMESPACE"))
	}

}
