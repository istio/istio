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

package main

import (
	"flag"

	"github.com/spf13/cobra/doc"

	"github.com/spf13/cobra"

	"istio.io/pkg/collateral"
	"istio.io/pkg/version"
)

// getRootCmd returns the root of the cobra command-tree.
func getRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "operator",
		Short: "The Istio operator.",
		Args:  cobra.ExactArgs(0),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.AddCommand(serverCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Operator",
		Section: "operator CLI",
		Manual:  "Istio Operator",
	}))

	return rootCmd
}
