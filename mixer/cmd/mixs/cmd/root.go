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

package cmd

import (
	"flag"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/collateral"
	"istio.io/pkg/version"
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, info map[string]template.Info, adapters []adapter.InfoFn,
	printf, fatalf shared.FormatFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "mixs",
		Short: "Mixer is Istio's abstraction on top of infrastructure backends.",
		Long: "Mixer is Istio's point of integration with infrastructure backends and is the\n" +
			"nexus for policy evaluation and telemetry reporting.",
		Args: cobra.ExactArgs(0),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.AddCommand(serverCmd(info, adapters, printf, fatalf))
	rootCmd.AddCommand(probeCmd(printf, fatalf))
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Mixer Server",
		Section: "mixs CLI",
		Manual:  "Istio Mixer Server",
	}))

	return rootCmd
}
