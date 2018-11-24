// Copyright 2018 Istio Authors
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

package cmd

import (
	"flag"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/operator/cmd/shared"
	"istio.io/istio/operator/pkg/server"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
)

var (
	flags = struct {
		kubeConfig   string
		resyncPeriod time.Duration
	}{}

	loggingOptions = log.DefaultOptions()
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, printf, fatalf shared.FormatFn) *cobra.Command {

	var (
		serverArgs               = server.DefaultArgs()
		livenessProbeOptions     probe.Options
		readinessProbeOptions    probe.Options
		livenessProbeController  probe.Controller
		readinessProbeController probe.Controller
		monitoringPort           uint
	)

	rootCmd := &cobra.Command{
		Use:          "operator",
		Short:        "Operator provides installating services for Istio on Kubernetes.",
		Long:         "Operator provides installation services for Istio on Kubernetes.",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%q is an invalid argument", args[0])
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			serverArgs.KubeConfig = flags.kubeConfig
			serverArgs.ResyncPeriod = flags.resyncPeriod
			serverArgs.LoggingOptions = loggingOptions
			if livenessProbeOptions.IsValid() {
				livenessProbeController = probe.NewFileController(&livenessProbeOptions)
			}
			if readinessProbeOptions.IsValid() {
				readinessProbeController = probe.NewFileController(&readinessProbeOptions)

			}

			if serverArgs.EnableServer {
//				go server.RunServer(serverArgs, printf, fatalf, nil, nil)
				go server.RunServer(serverArgs, printf, fatalf, livenessProbeController, readinessProbeController)
			}
//			operatorStop := make(chan struct{})
//			go server.StartProbeCheck(livenessProbeController, readinessProbeController, operatorStop)
			printf ("here")
//			istiocmd.WaitSignal(operatorStop)

		},
	}

	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.PersistentFlags().StringVar(&flags.kubeConfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().DurationVar(&flags.resyncPeriod, "resyncPeriod", 0,
		"Resync period for rescanning Kubernetes resources")
	rootCmd.PersistentFlags().StringVar(&livenessProbeOptions.Path, "livenessProbePath", server.DefaultLivenessProbeFilePath,
		"Path to the file for the Operator liveness probe.")
	rootCmd.PersistentFlags().DurationVar(&livenessProbeOptions.UpdateInterval, "livenessProbeInterval", server.DefaultProbeCheckInterval,
		"Interval of updating file for the Operator liveness probe.")
	rootCmd.PersistentFlags().StringVar(&readinessProbeOptions.Path, "readinessProbePath", server.DefaultReadinessProbeFilePath,
		"Path to the file for the Operator readiness probe.")
	rootCmd.PersistentFlags().DurationVar(&readinessProbeOptions.UpdateInterval, "readinessProbeInterval", server.DefaultProbeCheckInterval,
		"Interval of updating file for the Operator readiness probe.")
	rootCmd.PersistentFlags().UintVar(&monitoringPort, "monitoringPort", 9093,
		"Port to use for exposing self-monitoring information")

	//server config
	rootCmd.PersistentFlags().StringVarP(&serverArgs.APIAddress, "server-address", "", serverArgs.APIAddress,
		"Address to use for Operator's gRPC API, e.g. tcp://127.0.0.1:9092 or unix:///path/to/file")
	rootCmd.PersistentFlags().UintVarP(&serverArgs.MaxReceivedMessageSize, "server-maxReceivedMessageSize", "", serverArgs.MaxReceivedMessageSize,
		"Maximum size of individual gRPC messages")
	rootCmd.PersistentFlags().UintVarP(&serverArgs.MaxConcurrentStreams, "server-maxConcurrentStreams", "", serverArgs.MaxConcurrentStreams,
		"Maximum number of outstanding RPCs per connection")
	rootCmd.PersistentFlags().BoolVarP(&serverArgs.Insecure, "insecure", "", serverArgs.Insecure,
		"Use insecure gRPC communication")
	rootCmd.PersistentFlags().BoolVar(&serverArgs.EnableServer, "enable-server", serverArgs.EnableServer, "Run operator server mode")
	rootCmd.PersistentFlags().StringVarP(&serverArgs.AccessListFile, "accessListFile", "", serverArgs.AccessListFile,
		"The access list yaml file that contains the allowd mTLS peer ids.")
	rootCmd.PersistentFlags().StringVar(&serverArgs.ConfigPath, "configPath", serverArgs.ConfigPath,
		"Istio config file path")
	rootCmd.PersistentFlags().StringVar(&serverArgs.MeshConfigFile, "meshConfigFile", serverArgs.MeshConfigFile,
		"Path to the mesh config file")
	rootCmd.PersistentFlags().StringVar(&serverArgs.DomainSuffix, "domain", serverArgs.DomainSuffix,
		"DNS domain suffix")
	rootCmd.PersistentFlags().BoolVar(&serverArgs.DisableResourceReadyCheck, "disableResourceReadyCheck", serverArgs.DisableResourceReadyCheck,
		"Disable resource readiness checks. This allows Operator to start if not all resource types are supported")

	serverArgs.IntrospectionOptions.AttachCobraFlags(rootCmd)

	rootCmd.AddCommand(probeCmd(printf, fatalf))
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Operator Server",
		Section: "operator CLI",
		Manual:  "Istio Operator Server",
	}))

	loggingOptions.AttachCobraFlags(rootCmd)

	return rootCmd
}
