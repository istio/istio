// Copyright Istio Authors
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

package app

import (
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/cni/pkg/ambient"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cmd"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	loggingOptions = log.DefaultOptions()
	serverArgs     *ambient.AmbientArgs
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "ambient-ds",
		Short:        "Ambient DaemonSet",
		Long:         "Monitors mesh configuration and existing resources to manage the ipset lists",
		SilenceUsage: true,
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
		PreRunE: func(c *cobra.Command, args []string) error {
			cmd.AddFlags(c)
			return nil
		},
	}

	informerCmd := newInformerCommand()
	addFlags(informerCmd)
	rootCmd.AddCommand(informerCmd)

	return rootCmd
}

func addFlags(c *cobra.Command) {
	serverArgs = ambient.NewAmbientArgs()

	c.PersistentFlags().StringSliceVar(&serverArgs.RegistryOptions.Registries, "registries",
		[]string{string(provider.Kubernetes)},
		fmt.Sprintf("Comma separated list of platform service registries to read from (choose one or more from {%s, %s})",
			provider.Kubernetes, provider.Mock))
	c.PersistentFlags().StringVar(&serverArgs.RegistryOptions.KubeConfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	c.PersistentFlags().StringVar(&serverArgs.MeshConfigFile, "meshConfig", "./etc/istio/config/mesh",
		"File name for Istio mesh configuration. If not specified, a default mesh will be used.")
	c.PersistentFlags().Float32Var(&serverArgs.RegistryOptions.KubeOptions.KubernetesAPIQPS, "kubernetesApiQPS", 80.0,
		"Maximum QPS when communicating with the kubernetes API")
	c.PersistentFlags().IntVar(&serverArgs.RegistryOptions.KubeOptions.KubernetesAPIBurst, "kubernetesApiBurst", 160,
		"Maximum burst for throttle when communicating with the kubernetes API")

	loggingOptions.AttachCobraFlags(c)
}

func newInformerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "informer",
		Short: "Run the Ambient informer service",
		Long:  "Run the Ambient informer service",
		Args:  cobra.ExactArgs(0),
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
		PreRunE: func(c *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			log.Infof("Version %s", version.Info.String())

			stop := make(chan struct{})

			server, err := ambient.NewServer(serverArgs)
			if err != nil {
				return fmt.Errorf("failed to create ambient informer service: %v", err)
			}

			if err := server.Start(stop); err != nil {
				return fmt.Errorf("failed to start ambient informer service: %v", err)
			}

			cmd.WaitSignal(stop)
			server.WaitUntilCompletion()

			return nil
		},
	}
}
