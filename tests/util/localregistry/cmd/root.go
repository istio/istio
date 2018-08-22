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

	"istio.io/istio/tests/util/localregistry"
)

type localRegistrySetupArgs struct {
	remoteRegistryPort uint16
	kubeconfig         string
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "localregistry",
		Short: "Localregistry is a tool to setup in-cluster local registry in kubernetes environment",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(setupCfgCmd(args))
	rootCmd.AddCommand(teardownCfgCmd(args))

	return rootCmd
}


func setupCfgCmd(rawArgs []string) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "sets up localregistry in kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			if err := localregistry.SetupLocalRegistry(sa.remoteRegistryPort, sa.kubeconfig); err != nil {
				fmt.Println("Error Setting up local registry. err %v", err)
			} else {
				fmt.Println("Local Registry is Setup.")
			}
		},
	}
	setupCmd.PersistentFlags().Uint16Var(&sa.remoteRegistryPort, "remoteRegistryPort", 5000,
		"port number where registry can be port-forwarded on user's machine. Default is 5000, same as local "+
			"registry port.")
	setupCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return setupCmd
}

func teardownCfgCmd(rawArgs []string) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	teardownCmd := &cobra.Command{
		Use:   "delete",
		Short: "deletes localregistry in kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			if err := localregistry.TeardownLocalRegistry(sa.kubeconfig) ; err != nil {
				fmt.Println("Error Tearing down local registry. err %v", err)
			} else {
				fmt.Println("Local Registry Cleaned up")
			}
		},
	}
	teardownCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return teardownCmd
}