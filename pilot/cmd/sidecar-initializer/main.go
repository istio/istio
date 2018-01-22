// Copyright 2017 Istio Authors
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
	"os"

	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"

	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/platform/kube"
	"istio.io/istio/pilot/platform/kube/inject"
	"istio.io/istio/pilot/tools/version"
	"istio.io/istio/pkg/log"
)

var (
	flags = struct {
		kubeconfig     string
		meshconfig     string
		injectConfig   string
		namespace      string
		loggingOptions *log.Options
	}{
		loggingOptions: log.NewOptions(),
	}

	rootCmd = &cobra.Command{
		Use:   "sidecar-initializer",
		Short: "Kubernetes initializer for Istio sidecar",
		RunE: func(*cobra.Command, []string) error {
			if err := log.Configure(flags.loggingOptions); err != nil {
				return err
			}
			restConfig, client, err := kube.CreateInterface(flags.kubeconfig)
			if err != nil {
				return multierror.Prefix(err, "failed to connect to Kubernetes API.")
			}

			log.Infof("version %s", version.Line())

			config, err := inject.GetInitializerConfig(client, flags.namespace, flags.injectConfig)
			if err != nil {
				return multierror.Prefix(err, "failed to read initializer configuration")
			}

			// retrieve mesh configuration separately
			if config.Params.Mesh, err = cmd.ReadMeshConfig(flags.meshconfig); err != nil {
				return multierror.Prefix(err, "failed to read mesh configuration.")
			}

			initializer, err := inject.NewInitializer(restConfig, config, client)
			if err != nil {
				return multierror.Prefix(err, "failed to create initializer")
			}

			stop := make(chan struct{})

			go initializer.Run(stop)

			cmd.WaitSignal(stop)
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshconfig", "/etc/istio/config/mesh",
		"File name for Istio mesh configuration")
	rootCmd.PersistentFlags().StringVar(&flags.injectConfig, "injectConfig", "istio-inject",
		"Name of initializer configuration ConfigMap")
	rootCmd.PersistentFlags().StringVar(&flags.namespace, "namespace", v1.NamespaceDefault, // TODO istio-system?
		"Namespace of initializer configuration ConfigMap")

	// Attach the Istio logging options to the command.
	flags.loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)
}

func main() {
	// Needed to avoid "logging before flag.Parse" error with glog.
	cmd.SupressGlogWarnings()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
