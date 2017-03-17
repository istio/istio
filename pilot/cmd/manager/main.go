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

	"istio.io/manager/cmd"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
	"istio.io/manager/proxy/envoy"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

type args struct {
	proxy    envoy.MeshConfig
	identity envoy.ProxyNode
	sdsPort  int
}

const (
	resyncPeriod = 100 * time.Millisecond
)

var (
	flags = &args{}

	discoveryCmd = &cobra.Command{
		Use:   "discovery",
		Short: "Start Istio Manager discovery service",
		RunE: func(c *cobra.Command, args []string) (err error) {
			controller := kube.NewController(cmd.Client, cmd.RootFlags.Namespace, resyncPeriod)
			sds := envoy.NewDiscoveryService(controller,
				&model.IstioRegistry{ConfigRegistry: controller},
				&flags.proxy,
				flags.sdsPort)
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
			controller := kube.NewController(cmd.Client, cmd.RootFlags.Namespace, resyncPeriod)
			_, err = envoy.NewWatcher(controller,
				controller,
				&model.IstioRegistry{ConfigRegistry: controller},
				&flags.proxy,
				&flags.identity)
			if err != nil {
				return
			}
			stop := make(chan struct{})
			go controller.Run(stop)
			cmd.WaitSignal(stop)
			return
		},
	}

	ingressCmd = &cobra.Command{
		Use:   "ingress",
		Short: "Istio Proxy ingress controller",
		RunE: func(c *cobra.Command, args []string) error {
			setFlagsFromEnv()
			controller := kube.NewController(cmd.Client, cmd.RootFlags.Namespace, resyncPeriod)
			_, err := envoy.NewIngressWatcher(controller,
				controller,
				&model.IstioRegistry{ConfigRegistry: controller},
				&flags.proxy,
				&flags.identity)
			if err != nil {
				return err
			}
			stop := make(chan struct{})
			go controller.Run(stop)
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
	discoveryCmd.PersistentFlags().IntVarP(&flags.sdsPort, "port", "p", 8080,
		"Discovery service port")
	discoveryCmd.PersistentFlags().StringVarP(&flags.proxy.MixerAddress, "mixer", "m",
		"",
		"Mixer DNS address (or empty to disable Mixer)")

	proxyCmd.PersistentFlags().StringVar(&flags.identity.IP, "nodeIP", "",
		"Proxy node IP address. If not provided uses ${POD_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&flags.identity.Name, "nodeName", "",
		"Proxy node name. If not provided uses ${POD_NAME}.${POD_NAMESPACE}")

	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.DiscoveryAddress, "sds", "s",
		envoy.DefaultMeshConfig.DiscoveryAddress,
		"Discovery service DNS address")
	proxyCmd.PersistentFlags().IntVarP(&flags.proxy.ProxyPort, "port", "p",
		envoy.DefaultMeshConfig.ProxyPort,
		"Envoy proxy port")
	proxyCmd.PersistentFlags().IntVarP(&flags.proxy.AdminPort, "admin_port", "a",
		envoy.DefaultMeshConfig.AdminPort,
		"Envoy admin port")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.BinaryPath, "envoy_path", "b",
		envoy.DefaultMeshConfig.BinaryPath,
		"Envoy binary location")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.ConfigPath, "config_path", "e",
		envoy.DefaultMeshConfig.ConfigPath,
		"Envoy config root location")
	proxyCmd.PersistentFlags().StringVarP(&flags.proxy.MixerAddress, "mixer", "m",
		"",
		"Mixer DNS address (or empty to disable Mixer)")

	proxyCmd.AddCommand(sidecarCmd)
	proxyCmd.AddCommand(ingressCmd)
	proxyCmd.AddCommand(egressCmd)

	cmd.RootCmd.Use = "manager"
	cmd.RootCmd.Long = `
Istio Manager provides management plane functionality to the Istio proxy mesh and Istio Mixer.`
	cmd.RootCmd.AddCommand(discoveryCmd)
	cmd.RootCmd.AddCommand(proxyCmd)
}

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
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
