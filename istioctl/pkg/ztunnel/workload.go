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

package ztunnel

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/istioctl/pkg/proxyconfig"
	ambientutil "istio.io/istio/istioctl/pkg/util/ambient"
	ztunnelDump "istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/pkg/kube"
)

func workloadConfigCmd(ctx cli.Context) *cobra.Command {
	workloadConfigCmd := &cobra.Command{
		Use:   "workload [<type>/]<name>[.<namespace>]",
		Short: "Retrieves workload configuration for the specified ztunnel pod",
		Long:  `Retrieve information about workload configuration for the ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a given ztunnel.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]>

  # Retrieve summary of workloads on node XXXX for a given ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --node ambient-worker

  # Retrieve full workload dump of workloads with address XXXX for a given ztunnel
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --address 0.0.0.0 -o json

  # Retrieve workload summary
  kubectl exec -it $ZTUNNEL -n istio-system -- curl localhost:15000/config_dump > ztunnel-config.json
  istioctl ztunnel-config workloads --file ztunnel-config.json

  # Retrieve workload summary for a specific namespace
  istioctl ztunnel-config workloads <ztunnel-name[.namespace]> --workloads-namespace foo
`,
		Aliases: []string{"workloads", "w"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("workload requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}

			var podName, podNamespace string
			var configWriter *ztunnelDump.ConfigWriter
			if len(args) == 1 {
				if podName, podNamespace, err = getComponentPodName(ctx, args[0]); err != nil {
					return err
				}
				ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
				if !ztunnelPod {
					return fmt.Errorf("workloads command is only supported by ztunnel proxies: %v", podName)
				}
				configWriter, err = setupZtunnelConfigDumpWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
			} else {
				configWriter, err = setupFileZtunnelConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}
			filter := ztunnelDump.WorkloadFilter{
				Namespace: workloadsNamespace,
				Address:   address,
				Node:      node,
				Verbose:   verboseProxyConfig,
			}

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintWorkloadSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintWorkloadDump(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	workloadConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	workloadConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter workloads by address field")
	workloadConfigCmd.PersistentFlags().StringVar(&node, "node", "", "Filter workloads by node field")
	workloadConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	workloadConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Ztunnel config dump JSON file")
	workloadConfigCmd.PersistentFlags().StringVar(&workloadsNamespace, "workloads-namespace", "",
		"Filter workloads by namespace field")

	return workloadConfigCmd
}

func extractZtunnelConfigDump(kubeClient kube.CLIClient, podName, podNamespace string) ([]byte, error) {
	path := "config_dump"
	debug, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, proxyAdminPort)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on %s.%s ztunnel: %v", podName, podNamespace, err)
	}
	return debug, err
}

func setupZtunnelConfigDumpWriter(kubeClient kube.CLIClient, podName, podNamespace string, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	debug, err := extractZtunnelConfigDump(kubeClient, podName, podNamespace)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpZtunnelConfigWriter(debug, out)
}

func setupConfigdumpZtunnelConfigWriter(debug []byte, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	cw := &ztunnelDump.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

// getComponentPodName returns the pod name and namespace of the Istio component
func getComponentPodName(ctx cli.Context, podflag string) (string, string, error) {
	return proxyconfig.GetPodNameWithNamespace(ctx, podflag, ctx.IstioNamespace())
}

func setupFileZtunnelConfigdumpWriter(filename string, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	data, err := proxyconfig.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpZtunnelConfigWriter(data, out)
}
