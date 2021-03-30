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
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/galley/pkg/config/analysis/local"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/kube"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"
)


func upgradeCheckCommand() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var namespaces []string
	var allNamespaces, skipControlPlane bool
	// cmd represents the upgradeCheck command
	var cmd = &cobra.Command{
		Use:   "upgrade-check",
		Short: "check whether your istio installation can safely be upgraded",
		Long: `upgrade-check is a collection of checks to ensure that your Istio installation is ready to upgrade.  By 
default, it checks to ensure that your control plane is safe to upgrade, but you can check that the dataplane is safe 
to upgrade as well by specifying --namespaces to check, or using --all-namespaces.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipControlPlane {
				if err := checkControlPlane(cmd); err != nil {
					return err
				}
			}
			if allNamespaces {
				namespaces = []string{v1.NamespaceAll}
			}
			for _, ns := range namespaces {
				if err := checkDataPlane(cmd, ns); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringArrayVarP(&namespaces, "namespaces", "n", nil, "check the dataplane in these specific namespaces")
	cmd.PersistentFlags().BoolVarP(&allNamespaces, "all-namespaces", "a", false, "check the dataplane in all accessible namespaces")
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func checkControlPlane(cmd *cobra.Command) error {
	sa := local.NewSourceAnalyzer(schema.MustGet(), analysis.Combine("upgrade precheck", &maturity.AlphaAnalyzer{}),
		resource.Namespace(selectedNamespace), resource.Namespace(istioNamespace), nil, true, analysisTimeout)
	// Set up the kube client
	config := kube.BuildClientCmd(kubeconfig, configContext)
	restConfig, err := config.ClientConfig()
	if err != nil {
		return err
	}
	k := cfgKube.NewInterfaces(restConfig)
	sa.AddRunningKubeSource(k)
	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return err
	}
	outputMessages := result.Messages.SetDocRef("istioctl-analyze").FilterOutLowerThan(outputThreshold.Level)

	// Print all the messages to stdout in the specified format
	output, err := formatting.Print(outputMessages, msgOutputFormat, colorize)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), output)
	return nil
}

func checkDataPlane(cmd *cobra.Command, namespace string) error {
	// TODO: uncomment this once John's PR merges.
	//checkBinds(ns)
	return nil
}
