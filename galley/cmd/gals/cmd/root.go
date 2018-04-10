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
	"path"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

var (
	flags = struct {
		kubeConfig string
	}{}

	common = struct {
		client kubernetes.Interface
	}{}

	loggingOptions = log.DefaultOptions()
)

// resolveConfig checks whether to use the in-cluster or out-of-cluster config
func resolveConfig(kubeconfig string) (string, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		defaultCfg := path.Join(homedir.HomeDir(), ".kube/config")
		fmt.Printf("(3) %v\n", defaultCfg)
		if _, err := os.Stat(kubeconfig); err == nil {
			kubeconfig = defaultCfg
		}
	}
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("kubernetes configuration file %q does not exist", kubeconfig)
			} else {
				err = multierror.Append(err, fmt.Errorf("kubernetes configuration file %q", kubeconfig))
			}
			return "", err
		}
		// if it's an empty file, switch to in-cluster config
		if info.Size() == 0 {
			log.Info("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// createInterface is a helper function to create Kubernetes interface
func createInterface(kubeconfig string) (kubernetes.Interface, error) {
	kube, err := resolveConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kube)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, printf, fatalf shared.FormatFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "gals",
		Short: "Galley provides configuration management services for Istio.",
		Long:  "Galley provides configuration management services for Istio.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%q is an invalid argument", args[0])
			}

			client, err := createInterface(flags.kubeConfig)
			if err != nil {
				return fmt.Errorf("failed to connect to Kubernetes API: %v", err)
			}
			common.client = client
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.PersistentFlags().StringVar(&flags.kubeConfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")

	rootCmd.AddCommand(validatorCmd(printf, fatalf))
	rootCmd.AddCommand(probeCmd(printf, fatalf))
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Galley Server",
		Section: "gals CLI",
		Manual:  "Istio Galley Server",
	}))

	loggingOptions.AttachCobraFlags(rootCmd)

	return rootCmd
}
