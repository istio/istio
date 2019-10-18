// Copyright 2019 Istio Authors
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
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	cfgKube "istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/pkg/kube"
)

var (
	useKube      bool
	useDiscovery string
)

// Analyze command
// Once we're ready to move this functionality out of the "experimental" subtree, we should merge
// with `istioctl validate`. https://github.com/istio/istio/issues/16777
func Analyze() *cobra.Command {
	analysisCmd := &cobra.Command{
		Use:   "analyze <file>...",
		Short: "Analyze Istio configuration and print validation messages",
		Example: `
# Analyze yaml files
istioctl experimental analyze a.yaml b.yaml

# Analyze the current live cluster
istioctl experimental analyze -k

# Analyze the current live cluster, simulating the effect of applying additional yaml files
istioctl experimental analyze -k a.yaml b.yaml

# Analyze yaml files, overriding service discovery to enabled
istioctl experimental analyze -d true a.yaml b.yaml services.yaml

# Analyze the current live cluster, overriding service discovery to disabled
istioctl experimental analyze -k -d false
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := gatherFiles(args)
			if err != nil {
				return err
			}
			cancel := make(chan struct{})

			sd, err := serviceDiscovery()
			if err != nil {
				return err
			}

			// We use the "namespace" arg that's provided as part of root istioctl as a flag for specifying what namespace to use
			// for file resources that don't have one specified.
			// Note that the current implementation (in root.go) doesn't correctly default this value based on --context, so we do that ourselves
			// below since for the time being we want to keep changes isolated to experimental code. When we merge this into
			// istioctl validate (see https://github.com/istio/istio/issues/16777) we should look into fixing getDefaultNamespace in root
			// so it properly handles the --context option.
			selectedNamespace := namespace

			var k cfgKube.Interfaces
			if useKube {
				// Set up the kube client
				config := kube.BuildClientCmd(kubeconfig, configContext)
				restConfig, err := config.ClientConfig()
				if err != nil {
					return err
				}
				k = cfgKube.NewInterfaces(restConfig)

				// If a default namespace to inject in files hasn't been explicitly defined already, use whatever is specified in the kube context
				if selectedNamespace == "" {
					ns, _, err := config.Namespace()
					if err != nil {
						return err
					}
					selectedNamespace = ns
				}
			}

			// If default namespace to inject wasn't specified by the user or derived from the k8s context, just use the default.
			if selectedNamespace == "" {
				selectedNamespace = defaultNamespace
			}

			sa := local.NewSourceAnalyzer(metadata.MustGet(), analyzers.AllCombined(), selectedNamespace, nil, sd)

			// If we're using kube, use that as a base source.
			if k != nil {
				sa.AddRunningKubeSource(k)
			}

			// If files are provided, treat them (collectively) as a source.
			if len(files) > 0 {
				if err = sa.AddFileKubeSource(files); err != nil {
					return err
				}
			}

			messages, err := sa.Analyze(cancel)
			if err != nil {
				return err
			}

			for _, m := range messages {
				fmt.Fprintf(cmd.OutOrStdout(), "%v\n", m.String())
			}

			return nil
		},
	}

	analysisCmd.PersistentFlags().BoolVarP(&useKube, "use-kube", "k", false,
		"Use live Kubernetes cluster for analysis")
	analysisCmd.PersistentFlags().StringVarP(&useDiscovery, "discovery", "d", "",
		"'true' to enable service discovery, 'false' to disable it. "+
			"Defaults to true if --use-kube is set, false otherwise. "+
			"Analyzers requiring resources made available by enabling service discovery will be skipped.")

	return analysisCmd
}

func gatherFiles(args []string) ([]string, error) {
	var result []string
	for _, a := range args {
		if _, err := os.Stat(a); err != nil {
			return nil, fmt.Errorf("could not find file %q", a)
		}
		result = append(result, a)
	}
	return result, nil
}

func serviceDiscovery() (bool, error) {
	switch strings.ToLower(useDiscovery) {
	case "":
		return useKube, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid argument value for discovery")
	}
}
