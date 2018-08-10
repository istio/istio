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
	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, printf, fatalf shared.FormatFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "minikubeenv",
		Short: "minikubeenv is a tool to setup minikube environment for Istio Testing",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(setupCfgCmd(args, printf, fatalf))
	rootCmd.AddCommand(teardownCfgCmd(args, printf, fatalf))

	return rootCmd
}
