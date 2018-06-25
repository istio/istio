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

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
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
	rootCmd := &cobra.Command{
		Use:          "galley",
		Short:        "Galley provides configuration management services for Istio.",
		Long:         "Galley provides configuration management services for Istio.",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%q is an invalid argument", args[0])
			}

			return log.Configure(loggingOptions)
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.PersistentFlags().StringVar(&flags.kubeConfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")

	rootCmd.PersistentFlags().DurationVar(&flags.resyncPeriod, "resyncPeriod", 0,
		"Resync period for rescanning Kubernetes resources")

	rootCmd.AddCommand(serverCmd(printf, fatalf))
	rootCmd.AddCommand(validatorCmd(printf, fatalf))
	rootCmd.AddCommand(probeCmd(printf, fatalf))
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Galley Server",
		Section: "galley CLI",
		Manual:  "Istio Galley Server",
	}))

	loggingOptions.AttachCobraFlags(rootCmd)

	return rootCmd
}
