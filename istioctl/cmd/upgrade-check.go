// Copyright © 2021 NAME HERE <EMAIL ADDRESS>
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
	"os"

	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/kube"
)

func upgradeCheckCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var namespaces []string
	var allNamespaces, skipControlPlane bool
	// cmd represents the upgradeCheck command
	cmd := &cobra.Command{
		Use:   "upgrade-check",
		Short: "check whether your istio installation can safely be upgraded",
		Long: `upgrade-check is a collection of checks to ensure that your Istio installation is ready to upgrade.  By 
default, it checks to ensure that your control plane is safe to upgrade, but you can check that the dataplane is safe 
to upgrade as well by specifying --namespaces to check, or using --all-namespaces.`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			msgs := diag.Messages{}
			if !skipControlPlane {
				msgs, err = checkControlPlane()
				if err != nil {
					return err
				}
			}
			if allNamespaces {
				namespaces = []string{v1.NamespaceAll}
			}
			if len(namespaces) < 1 {
				fmt.Fprintln(cmd.OutOrStdout(), "WARNING: no namespaces selected for dataplane upgrade checks.")
			}
			for _, ns := range namespaces {
				nsmsgs, err := checkDataPlane(ns)
				if err != nil {
					return err
				}
				msgs.Add(nsmsgs...)

			}
			// Print all the messages to stdout in the specified format
			output, err := formatting.Print(msgs.SortedDedupedCopy(), msgOutputFormat, colorize)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), output)
			if len(msgs) > 0 {
				os.Exit(2)
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringArrayVar(&namespaces, "namespaces", nil, "check the dataplane in these specific namespaces")
	cmd.PersistentFlags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "check the dataplane in all accessible namespaces")
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func checkControlPlane() (msgs diag.Messages, err error) {
	sa := local.NewSourceAnalyzer(schema.MustGet(), analysis.Combine("upgrade precheck", &maturity.AlphaAnalyzer{}),
		resource.Namespace(selectedNamespace), resource.Namespace(istioNamespace), nil, true, analysisTimeout)
	// Set up the kube client
	config := kube.BuildClientCmd(kubeconfig, configContext)
	restConfig, err := config.ClientConfig()
	if err != nil {
		return
	}
	k := cfgKube.NewInterfaces(restConfig)
	sa.AddRunningKubeSource(k)
	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if result.Messages != nil {
		msgs = result.Messages
	}
	return
}

func checkDataPlane(_ string) (diag.Messages, error) {
	// TODO: uncomment this once John's PR merges.
	// checkBinds(ns)
	return nil, nil
}
