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
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/viper"

	"istio.io/pkg/collateral"
	"istio.io/pkg/version"
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {

	rootCmd := &cobra.Command{
		Use:          "galley",
		Short:        "Galley provides configuration management services for Istio.",
		Long:         "Galley provides configuration management services for Istio.",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(0),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	var cfgFile string
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Config file containing args")

	cobra.OnInitialize(func() {
		if len(cfgFile) > 0 {
			viper.SetConfigFile(cfgFile)
			err := viper.ReadInConfig() // Find and read the config file
			if err != nil {             // Handle errors reading the config file
				os.Stderr.WriteString(fmt.Errorf("fatal error in config file: %s", err).Error())
				os.Exit(1)
			}
		}
	})

	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.AddCommand(serverCmd())
	rootCmd.AddCommand(probeCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Galley Server",
		Section: "galley CLI",
		Manual:  "Istio Galley Server",
	}))

	loggingOptions.AttachCobraFlags(rootCmd)

	return rootCmd
}
