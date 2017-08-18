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
	"fmt"
	"os"
	"time"

	"k8s.io/api/core/v1"

	"istio.io/pilot/cmd"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/platform/kube/inject"
	"istio.io/pilot/tools/version"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
)

func getRootCmd() *cobra.Command {
	flags := struct {
		kubeconfig   string
		meshconfig   string
		hub          string
		tag          string
		namespace    string
		policy       string
		resyncPeriod time.Duration
	}{}

	rootCmd := &cobra.Command{
		Use:   "sidecar-initializer",
		Short: "Kubernetes initializer for Istio sidecar",
		RunE: func(*cobra.Command, []string) error {
			switch inject.InjectionPolicy(flags.policy) {
			case inject.InjectionPolicyOff, inject.InjectionPolicyOptIn, inject.InjectionPolicyOptOut:
			default:
				return fmt.Errorf("unknown injection policy: %v", flags.policy)
			}

			client, err := kube.CreateInterface(flags.kubeconfig)
			if err != nil {
				return multierror.Prefix(err, "failed to connect to Kubernetes API.")
			}

			glog.V(2).Infof("version %s", version.Line())
			glog.V(2).Infof("flags %s", spew.Sdump(flags))

			// receive mesh configuration
			mesh, err := cmd.ReadMeshConfig(flags.meshconfig)
			if err != nil {
				return multierror.Prefix(err, "failed to read mesh configuration.")
			}

			options := inject.InitializerOptions{
				ResyncPeriod: flags.resyncPeriod,
				Hub:          flags.hub,
				Tag:          flags.tag,
				Namespace:    flags.namespace,

				InjectionPolicy: inject.InjectionPolicy(flags.policy),
			}
			initializer := inject.NewInitializer(client, mesh, options)

			stop := make(chan struct{})
			go initializer.Run(stop)
			cmd.WaitSignal(stop)

			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&flags.hub, "hub", "docker.io/istio", "Docker hub")
	rootCmd.PersistentFlags().StringVar(&flags.tag, "tag", "0.2", "Docker tag")
	rootCmd.PersistentFlags().StringVar(&flags.namespace, "namespace",
		v1.NamespaceAll, "Namespace managed by initializer")
	rootCmd.PersistentFlags().StringVar(&flags.policy, "policy",
		string(inject.InjectionPolicyOff), "default injection policy")

	rootCmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	rootCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshconfig", "/etc/istio/config/mesh",
		fmt.Sprintf("File name for Istio mesh configuration"))
	rootCmd.PersistentFlags().DurationVar(&flags.resyncPeriod, "resync", 6*time.Minute,
		"Initializers resync interval")

	cmd.AddFlags(rootCmd)

	return rootCmd
}

func main() {
	if err := getRootCmd().Execute(); err != nil {
		os.Exit(-1)
	}
}
