// Copyright 2016 Google Inc.
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

	"istio.io/manager/platform/kube"
	"istio.io/manager/proxy/envoy"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

type args struct {
	kubeconfig string
	namespace  string
	sdsPort    int
	sdsAddress string
	envoy      string
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "manager",
		Short: "Istio manager service",
		Long: `
The Istio manager service provides management plane functionality to
the Istio proxies and the Istio mixer.`,
	}

	sa := &args{}
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Start the server",
		Run: func(cmd *cobra.Command, args []string) {
			glog.V(2).Infof("server arguments: %#v", sa)

			client, err := kube.NewClient(sa.kubeconfig, nil)
			check("Failed to connect to Kubernetes API", err)

			err = client.RegisterResources()
			check("Failed to register Third-Party Resources", err)

			controller := kube.NewController(client, sa.namespace, 256*time.Millisecond)

			sds := envoy.NewDiscoveryService(controller, sa.sdsPort)

			// wait for a signal
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

			stop := make(chan struct{})
			go controller.Run(stop)
			go sds.Run()

			<-sigs
			close(stop)
			glog.Flush()
		},
	}
	serverCmd.PersistentFlags().IntVarP(&sa.sdsPort, "port", "p", 8080,
		"Discovery service port")
	rootCmd.AddCommand(serverCmd)

	proxyCmd := &cobra.Command{
		Use:   "proxy",
		Short: "Start the proxy agent",
		Run: func(cmd *cobra.Command, args []string) {
			client, err := kube.NewClient(sa.kubeconfig, nil)
			check("Failed to connect to Kubernetes API", err)

			controller := kube.NewController(client, sa.namespace, 256*time.Millisecond)
			stop := make(chan struct{})
			_ = envoy.NewWatcher(controller, controller, sa.sdsAddress, sa.envoy)
			controller.Run(stop)
		},
	}
	proxyCmd.PersistentFlags().StringVarP(&sa.sdsAddress, "sds", "s", "localhost:8080",
		"Discovery service external address")
	proxyCmd.PersistentFlags().StringVarP(&sa.envoy, "envoy", "e", "/usr/local/bin/envoy",
		"Envoy binary location")
	rootCmd.AddCommand(proxyCmd)

	rootCmd.PersistentFlags().StringVarP(&sa.kubeconfig, "kubeconfig", "c", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVarP(&sa.namespace, "namespace", "n", "",
		"Monitor the specified namespace instead of all namespaces")
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	check("Execution error", rootCmd.Execute())
}

func check(msg string, err error) {
	if err != nil {
		glog.Errorf("%s: %v", msg, err)
		os.Exit(-1)
	}
}
