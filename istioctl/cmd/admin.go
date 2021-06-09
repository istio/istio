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
	"fmt"

	"github.com/spf13/cobra"
)

func adminDashboard() *cobra.Command {
	return &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"dash", "d"},
		Short:   "Access to Istio web UIs",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown dashboard %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}
}

func adminCmd() *cobra.Command {
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage control plane (istiod) configuration",
		Long:  "A group of commands used to manage istiod configuration",
		Example: `  # Retrieve information about istiod configuration.
  istioctl admin log`,
		Aliases: []string{"istiod"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	istiodLog := istiodLogCmd()
	adminCmd.AddCommand(istiodLog)
	adminCmd.AddCommand(adminDashboard())
	adminCmd.AddCommand(statusCommand())
	adminCmd.PersistentFlags().StringVarP(&istiodLabelSelector, "selector", "l", "app=istiod", "label selector")

	adminExperimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}
	adminCmd.AddCommand(adminExperimentalCmd)

	return adminCmd
}
