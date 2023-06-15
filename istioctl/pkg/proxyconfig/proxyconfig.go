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

package proxyconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/istioctl/pkg/kubeinject"
	istioctlutil "istio.io/istio/istioctl/pkg/util"
	ambientutil "istio.io/istio/istioctl/pkg/util/ambient"
	"istio.io/istio/istioctl/pkg/writer"
	sdscompare "istio.io/istio/istioctl/pkg/writer/compare/sds"
	"istio.io/istio/istioctl/pkg/writer/envoy/clusters"
	"istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	ztunnelDump "istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

const (
	jsonOutput             = "json"
	yamlOutput             = "yaml"
	summaryOutput          = "short"
	prometheusOutput       = "prom"
	prometheusMergedOutput = "prom-merged"

	defaultProxyAdminPort = 15000
)

var (
	fqdn, direction, subset string
	port                    int
	verboseProxyConfig      bool
	waypointProxyConfig     bool

	address, listenerType, statsType, node string

	routeName string

	clusterName, status string

	// output format (yaml or short)
	outputFormat string

	proxyAdminPort int

	configDumpFile string

	labelSelector = ""
	name          string
)

// Level is an enumeration of all supported log levels.
type Level int

const (
	defaultLoggerName       = "level"
	defaultEnvoyOutputLevel = WarningLevel
)

const (
	// OffLevel disables logging
	OffLevel Level = iota
	// CriticalLevel enables critical level logging
	CriticalLevel
	// ErrorLevel enables error level logging
	ErrorLevel
	// WarningLevel enables warning level logging
	WarningLevel
	// InfoLevel enables info level logging
	InfoLevel
	// DebugLevel enables debug level logging
	DebugLevel
	// TraceLevel enables trace level logging
	TraceLevel
)

var levelToString = map[Level]string{
	TraceLevel:    "trace",
	DebugLevel:    "debug",
	InfoLevel:     "info",
	WarningLevel:  "warning",
	ErrorLevel:    "error",
	CriticalLevel: "critical",
	OffLevel:      "off",
}

var stringToLevel = map[string]Level{
	"trace":    TraceLevel,
	"debug":    DebugLevel,
	"info":     InfoLevel,
	"warning":  WarningLevel,
	"warn":     WarningLevel,
	"error":    ErrorLevel,
	"critical": CriticalLevel,
	"off":      OffLevel,
}

var (
	loggerLevelString = ""
	reset             = false
)

func ztunnelLogLevel(level string) string {
	switch level {
	case "warning":
		return "warn"
	case "critical":
		return "error"
	default:
		return level
	}
}

func extractConfigDump(kubeClient kube.CLIClient, podName, podNamespace string, eds bool) ([]byte, error) {
	path := "config_dump"
	if eds {
		path += "?include_eds=true"
	}
	debug, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, proxyAdminPort)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on %s.%s sidecar: %v", podName, podNamespace, err)
	}
	return debug, err
}

func extractZtunnelConfigDump(kubeClient kube.CLIClient, podName, podNamespace string) ([]byte, error) {
	path := "config_dump"
	debug, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, 15000)
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

func setupPodConfigdumpWriter(kubeClient kube.CLIClient, podName, podNamespace string, includeEds bool, out io.Writer) (*configdump.ConfigWriter, error) {
	debug, err := extractConfigDump(kubeClient, podName, podNamespace, includeEds)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpEnvoyConfigWriter(debug, out)
}

func readFile(filename string) ([]byte, error) {
	file := os.Stdin
	if filename != "-" {
		var err error
		file, err = os.Open(filename)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Errorf("failed to close %s: %s", filename, err)
		}
	}()
	return io.ReadAll(file)
}

func setupFileZtunnelConfigdumpWriter(filename string, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpZtunnelConfigWriter(data, out)
}

func setupFileConfigdumpWriter(filename string, out io.Writer) (*configdump.ConfigWriter, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpEnvoyConfigWriter(data, out)
}

func setupConfigdumpZtunnelConfigWriter(debug []byte, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	cw := &ztunnelDump.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

func setupConfigdumpEnvoyConfigWriter(debug []byte, out io.Writer) (*configdump.ConfigWriter, error) {
	cw := &configdump.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

func setupEnvoyClusterStatsConfig(kubeClient kube.CLIClient, podName, podNamespace string, outputFormat string) (string, error) {
	path := "clusters"
	if outputFormat == jsonOutput || outputFormat == yamlOutput {
		// for yaml output we will convert the json to yaml when printed
		path += "?format=json"
	}
	result, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, proxyAdminPort)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func setupEnvoyServerStatsConfig(kubeClient kube.CLIClient, podName, podNamespace string, outputFormat string) (string, error) {
	path := "stats"
	port := 15000
	if outputFormat == jsonOutput || outputFormat == yamlOutput {
		// for yaml output we will convert the json to yaml when printed
		path += "?format=json"
	} else if outputFormat == prometheusOutput {
		path += "/prometheus"
	} else if outputFormat == prometheusMergedOutput {
		pod, err := kubeClient.Kube().CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to retrieve Pod %s/%s: %v", podNamespace, podName, err)
		}

		promPath, promPort, err := util.PrometheusPathAndPort(pod)
		if err != nil {
			return "", fmt.Errorf("failed to retrieve prometheus path and port from Pod %s/%s: %v", podNamespace, podName, err)
		}
		path = promPath
		port = promPort
	}

	result, err := kubeClient.EnvoyDoWithPort(context.Background(), podName, podNamespace, "GET", path, port)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func setupEnvoyLogConfig(kubeClient kube.CLIClient, param, podName, podNamespace string) (string, error) {
	path := "logging"
	if param != "" {
		path = path + "?" + param
	}
	result, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "POST", path, proxyAdminPort)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func getLogLevelFromConfigMap(ctx cli.Context) (string, error) {
	valuesConfig, err := kubeinject.GetValuesFromConfigMap(ctx, "")
	if err != nil {
		return "", err
	}
	var values struct {
		SidecarInjectorWebhook struct {
			Global struct {
				Proxy struct {
					LogLevel string `json:"logLevel"`
				} `json:"proxy"`
			} `json:"global"`
		} `json:"sidecarInjectorWebhook"`
	}
	if err := yaml.Unmarshal([]byte(valuesConfig), &values); err != nil {
		return "", fmt.Errorf("failed to parse values config: %v [%v]", err, valuesConfig)
	}
	return values.SidecarInjectorWebhook.Global.Proxy.LogLevel, nil
}

func setupPodClustersWriter(kubeClient kube.CLIClient, podName, podNamespace string, out io.Writer) (*clusters.ConfigWriter, error) {
	path := "clusters?format=json"
	debug, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, proxyAdminPort)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return setupClustersEnvoyConfigWriter(debug, out)
}

func setupFileClustersWriter(filename string, out io.Writer) (*clusters.ConfigWriter, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	return setupClustersEnvoyConfigWriter(data, out)
}

// TODO(fisherxu): migrate this to config dump when implemented in Envoy
// Issue to track -> https://github.com/envoyproxy/envoy/issues/3362
func setupClustersEnvoyConfigWriter(debug []byte, out io.Writer) (*clusters.ConfigWriter, error) {
	cw := &clusters.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

func clusterConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	clusterConfigCmd := &cobra.Command{
		Use:   "cluster [<type>/]<name>[.<namespace>]",
		Short: "Retrieves cluster configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about cluster configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve summary about cluster configuration for a given pod from Envoy.
  istioctl proxy-config clusters <pod-name[.namespace]>

  # Retrieve cluster summary for clusters with port 9080.
  istioctl proxy-config clusters <pod-name[.namespace]> --port 9080

  # Retrieve full cluster dump for clusters that are inbound with a FQDN of details.default.svc.cluster.local.
  istioctl proxy-config clusters <pod-name[.namespace]> --fqdn details.default.svc.cluster.local --direction inbound -o json

  # Retrieve cluster summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config clusters --file envoy-config.json
`,
		Aliases: []string{"clusters", "c"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("cluster requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			var configWriter *configdump.ConfigWriter
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, false, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}
			filter := configdump.ClusterFilter{
				FQDN:      host.Name(fqdn),
				Port:      port,
				Subset:    subset,
				Direction: model.TrafficDirection(direction),
			}
			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintClusterSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintClusterDump(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	clusterConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	clusterConfigCmd.PersistentFlags().StringVar(&fqdn, "fqdn", "", "Filter clusters by substring of Service FQDN field")
	clusterConfigCmd.PersistentFlags().StringVar(&direction, "direction", "", "Filter clusters by Direction field")
	clusterConfigCmd.PersistentFlags().StringVar(&subset, "subset", "", "Filter clusters by substring of Subset field")
	clusterConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter clusters by Port field")
	clusterConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return clusterConfigCmd
}

func allConfigCmd(ctx cli.Context) *cobra.Command {
	allConfigCmd := &cobra.Command{
		Use:   "all [<type>/]<name>[.<namespace>]",
		Short: "Retrieves all configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about all configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve summary about all configuration for a given pod from Envoy.
  istioctl proxy-config all <pod-name[.namespace]>

  # Retrieve full cluster dump as JSON
  istioctl proxy-config all <pod-name[.namespace]> -o json

  # Retrieve full cluster dump with short syntax
  istioctl pc a <pod-name[.namespace]>

  # Retrieve cluster summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config all --file envoy-config.json
`,
		Aliases: []string{"a"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("all requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			switch outputFormat {
			case jsonOutput, yamlOutput:
				var dump []byte
				var err error
				if len(args) == 1 {
					podName, podNamespace, err := getPodName(ctx, args[0])
					if err != nil {
						return err
					}
					ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
					if ztunnelPod {
						dump, err = extractZtunnelConfigDump(kubeClient, podName, podNamespace)
					} else {
						dump, err = extractConfigDump(kubeClient, podName, podNamespace, false)
					}
					if err != nil {
						return err
					}
				} else {
					dump, err = readFile(configDumpFile)
					if err != nil {
						return err
					}
				}
				if outputFormat == yamlOutput {
					if dump, err = yaml.JSONToYAML(dump); err != nil {
						return err
					}
				}
				fmt.Fprintln(c.OutOrStdout(), string(dump))

			case summaryOutput:
				var configWriter *configdump.ConfigWriter
				if len(args) == 1 {
					podName, podNamespace, err := getPodName(ctx, args[0])
					if err != nil {
						return err
					}

					ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
					if ztunnelPod {
						w, err := setupZtunnelConfigDumpWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
						if err != nil {
							return err
						}

						return w.PrintFullSummary(ztunnelDump.WorkloadFilter{
							Address: address,
							Node:    node,
							Verbose: verboseProxyConfig,
						})
					}

					configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, true, c.OutOrStdout())
					if err != nil {
						return err
					}
				} else {
					var err error
					configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
					if err != nil {
						return err
					}
				}
				configdump.SetPrintConfigTypeInSummary(true)
				sdscompare.SetPrintConfigTypeInSummary(true)
				return configWriter.PrintFullSummary(
					configdump.ClusterFilter{
						FQDN:      host.Name(fqdn),
						Port:      port,
						Subset:    subset,
						Direction: model.TrafficDirection(direction),
					},
					configdump.ListenerFilter{
						Address: address,
						Port:    uint32(port),
						Type:    listenerType,
						Verbose: verboseProxyConfig,
					},
					configdump.RouteFilter{
						Name:    routeName,
						Verbose: verboseProxyConfig,
					},
					configdump.EndpointFilter{
						Address: address,
						Port:    uint32(port),
						Cluster: clusterName,
						Status:  status,
					},
				)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	allConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	allConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump file")
	allConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")

	// cluster
	allConfigCmd.PersistentFlags().StringVar(&fqdn, "fqdn", "", "Filter clusters by substring of Service FQDN field")
	allConfigCmd.PersistentFlags().StringVar(&direction, "direction", "", "Filter clusters by Direction field")
	allConfigCmd.PersistentFlags().StringVar(&subset, "subset", "", "Filter clusters by substring of Subset field")

	// applies to cluster and route
	allConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter clusters and listeners by Port field")

	// Listener
	allConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter listeners by address field")
	allConfigCmd.PersistentFlags().StringVar(&listenerType, "type", "", "Filter listeners by type field")

	// route
	allConfigCmd.PersistentFlags().StringVar(&routeName, "name", "", "Filter listeners by route name field")

	return allConfigCmd
}

func workloadConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	workloadConfigCmd := &cobra.Command{
		// Until stabilized
		Hidden: true,
		Use:    "workload [<type>/]<name>[.<namespace>]",
		Short:  "Retrieves workload configuration for the specified ztunnel pod",
		Long:   `Retrieve information about workload configuration for the ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a given ztunnel.
  istioctl proxy-config workload <ztunnel-name[.namespace]>

  # Retrieve summary of workloads on node XXXX for a given ztunnel instance.
  istioctl proxy-config workload <ztunnel-name[.namespace]> --node ambient-worker

  # Retrieve full workload dump of workloads with address XXXX for a given ztunnel
  istioctl proxy-config workload <ztunnel-name[.namespace]> --address 0.0.0.0 -o json

  # Retrieve workload summary
  kubectl exec -it $ZTUNNEL -n istio-system -- curl localhost:15000/config_dump > ztunnel-config.json
  istioctl proxy-config workloads --file ztunnel-config.json
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
				Address: address,
				Node:    node,
				Verbose: verboseProxyConfig,
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
		"ztunnel config dump JSON file")

	return workloadConfigCmd
}

func listenerConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	listenerConfigCmd := &cobra.Command{
		Use:   "listener [<type>/]<name>[.<namespace>]",
		Short: "Retrieves listener configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about listener configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve summary about listener configuration for a given pod from Envoy.
  istioctl proxy-config listeners <pod-name[.namespace]>

  # Retrieve listener summary for listeners with port 9080.
  istioctl proxy-config listeners <pod-name[.namespace]> --port 9080

  # Retrieve full listener dump for HTTP listeners with a wildcard address (0.0.0.0).
  istioctl proxy-config listeners <pod-name[.namespace]> --type HTTP --address 0.0.0.0 -o json

  # Retrieve listener summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config listeners --file envoy-config.json
`,
		Aliases: []string{"listeners", "l"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("listener requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			var configWriter *configdump.ConfigWriter
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, false, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}
			filter := configdump.ListenerFilter{
				Address: address,
				Port:    uint32(port),
				Type:    listenerType,
				Verbose: verboseProxyConfig,
			}

			if waypointProxyConfig {
				return configWriter.PrintRemoteListenerSummary()
			}
			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintListenerSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintListenerDump(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	listenerConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	listenerConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter listeners by address field")
	listenerConfigCmd.PersistentFlags().StringVar(&listenerType, "type", "", "Filter listeners by type field")
	listenerConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter listeners by Port field")
	listenerConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	listenerConfigCmd.PersistentFlags().BoolVar(&waypointProxyConfig, "waypoint", false, "Output waypoint information")
	// Until stabilized
	_ = listenerConfigCmd.PersistentFlags().MarkHidden("waypoint")
	listenerConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return listenerConfigCmd
}

func StatsConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	statsConfigCmd := &cobra.Command{
		Use:   "envoy-stats [<type>/]<name>[.<namespace>]",
		Short: "Retrieves Envoy metrics in the specified pod",
		Long:  `Retrieve Envoy emitted metrics for the specified pod.`,
		Example: `  # Retrieve Envoy emitted metrics for the specified pod.
  istioctl experimental envoy-stats <pod-name[.namespace]>

  # Retrieve Envoy server metrics in prometheus format
  istioctl experimental envoy-stats <pod-name[.namespace]> --output prom

  # Retrieve Envoy server metrics in prometheus format with merged application metrics
  istioctl experimental envoy-stats <pod-name[.namespace]> --output prom-merged

  # Retrieve Envoy cluster metrics
  istioctl experimental envoy-stats <pod-name[.namespace]> --type clusters
`,
		Aliases: []string{"es"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 && (labelSelector == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("stats requires pod name or label selector")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var stats string
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
				return err
			}
			if statsType == "" || statsType == "server" {
				stats, err = setupEnvoyServerStatsConfig(kubeClient, podName, podNamespace, outputFormat)
				if err != nil {
					return err
				}
			} else if statsType == "cluster" || statsType == "clusters" {
				stats, err = setupEnvoyClusterStatsConfig(kubeClient, podName, podNamespace, outputFormat)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("unknown stats type %s", statsType)
			}

			switch outputFormat {
			// convert the json output to yaml
			case yamlOutput:
				var out []byte
				if out, err = yaml.JSONToYAML([]byte(stats)); err != nil {
					return err
				}
				_, _ = fmt.Fprint(c.OutOrStdout(), string(out))
			default:
				_, _ = fmt.Fprint(c.OutOrStdout(), stats)
			}

			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}
	statsConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|prom|prom-merged")
	statsConfigCmd.PersistentFlags().StringVarP(&statsType, "type", "t", "server", "Where to grab the stats: one of server|clusters")

	return statsConfigCmd
}

type proxyType int

const (
	Envoy proxyType = iota
	Ztunnel
)

func logCmd(ctx cli.Context) *cobra.Command {
	var podNamespace string
	var podNames []string

	logCmd := &cobra.Command{
		Use:   "log [<type>/]<name>[.<namespace>]",
		Short: "(experimental) Retrieves logging levels of the Envoy in the specified pod",
		Long:  "(experimental) Retrieve information about logging levels of the Envoy instance in the specified pod, and update optionally",
		Example: `  # Retrieve information about logging levels for a given pod from Envoy.
  istioctl proxy-config log <pod-name[.namespace]>

  # Update levels of the all loggers
  istioctl proxy-config log <pod-name[.namespace]> --level none

  # Update levels of the specified loggers.
  istioctl proxy-config log <pod-name[.namespace]> --level http:debug,redis:debug

  # Reset levels of all the loggers to default value (warning).
  istioctl proxy-config log <pod-name[.namespace]> -r
`,
		Aliases: []string{"o"},
		Args: func(cmd *cobra.Command, args []string) error {
			if labelSelector == "" && len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("log requires pod name or --selector")
			}
			if reset && loggerLevelString != "" {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("--level cannot be combined with --reset")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			loggerNames := map[string]proxyType{}
			if labelSelector != "" {
				if podNames, podNamespace, err = getPodNameBySelector(ctx, kubeClient, labelSelector); err != nil {
					return err
				}
			} else {
				if podNames, podNamespace, err = getPodNames(ctx, args[0], ctx.Namespace()); err != nil {
					return err
				}
			}
			for _, pod := range podNames {
				name, err = setupEnvoyLogConfig(kubeClient, "", pod, podNamespace)
				if err != nil {
					return err
				}
				ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, pod, podNamespace)
				if ztunnelPod {
					loggerNames[name] = Ztunnel
				} else {
					loggerNames[name] = Envoy
				}
			}

			destLoggerLevels := map[string]Level{}
			if reset {
				// reset logging level to `defaultOutputLevel`, and ignore the `level` option
				levelString, _ := getLogLevelFromConfigMap(ctx)
				level, ok := stringToLevel[levelString]
				if ok {
					destLoggerLevels[defaultLoggerName] = level
				} else {
					log.Warnf("unable to get logLevel from ConfigMap istio-sidecar-injector, using default value %q for envoy proxies and \"info\" for ztunnel",
						levelToString[defaultEnvoyOutputLevel])
					destLoggerLevels[defaultLoggerName] = defaultEnvoyOutputLevel
				}
			} else if loggerLevelString != "" {
				levels := strings.Split(loggerLevelString, ",")
				for _, ol := range levels {
					if !strings.Contains(ol, ":") && !strings.Contains(ol, "=") {
						level, ok := stringToLevel[ol]
						if ok {
							destLoggerLevels = map[string]Level{
								defaultLoggerName: level,
							}
						} else {
							return fmt.Errorf("unrecognized logging level: %v", ol)
						}
					} else {
						logParts := strings.Split(ol, "::") // account for any specified namespace
						loggerAndLevelOnly := logParts[len(logParts)-1]
						loggerLevel := regexp.MustCompile(`[:=]`).Split(loggerAndLevelOnly, 2)
						for logName, typ := range loggerNames {
							if typ == Ztunnel {
								// TODO validate ztunnel logger name when available: https://github.com/istio/ztunnel/issues/426
								continue
							}
							if !strings.Contains(logName, loggerLevel[0]) {
								return fmt.Errorf("unrecognized logger name: %v", loggerLevel[0])
							}
						}
						level, ok := stringToLevel[loggerLevel[1]]
						if !ok {
							return fmt.Errorf("unrecognized logging level: %v", loggerLevel[1])
						}
						destLoggerLevels[loggerLevel[0]] = level
					}
				}
			}

			var resp string
			var errs *multierror.Error
			for _, podName := range podNames {
				ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
				if ztunnelPod {
					q := "level=" + ztunnelLogLevel(loggerLevelString)
					if reset {
						q += "&reset"
					}
					resp, err := setupEnvoyLogConfig(kubeClient, q, podName, podNamespace)
					if err == nil {
						_, _ = fmt.Fprintf(c.OutOrStdout(), "%v.%v:\n%v\n", podName, podNamespace, resp)
					} else {
						errs = multierror.Append(fmt.Errorf("%v.%v: %v", podName, podNamespace, err))
					}
					continue
				}
				if len(destLoggerLevels) == 0 {
					resp, err = setupEnvoyLogConfig(kubeClient, "", podName, podNamespace)
				} else {
					if ll, ok := destLoggerLevels[defaultLoggerName]; ok {
						// update levels of all loggers first
						resp, err = setupEnvoyLogConfig(kubeClient, defaultLoggerName+"="+levelToString[ll], podName, podNamespace)
					}
					for lg, ll := range destLoggerLevels {
						if lg == defaultLoggerName {
							continue
						}
						resp, err = setupEnvoyLogConfig(kubeClient, lg+"="+levelToString[ll], podName, podNamespace)
					}
				}
				if err != nil {
					errs = multierror.Append(errs, fmt.Errorf("error configuring log level for %v.%v: %v", podName, podNamespace, err))
				} else {
					_, _ = fmt.Fprintf(c.OutOrStdout(), "%v.%v:\n%v", podName, podNamespace, resp)
				}
			}
			if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
				return err
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	levelListString := fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s]",
		levelToString[TraceLevel],
		levelToString[DebugLevel],
		levelToString[InfoLevel],
		levelToString[WarningLevel],
		levelToString[ErrorLevel],
		levelToString[CriticalLevel],
		levelToString[OffLevel])

	logCmd.PersistentFlags().BoolVarP(&reset, "reset", "r", reset, "Reset levels to default value (warning).")
	logCmd.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "Label selector")
	logCmd.PersistentFlags().StringVar(&loggerLevelString, "level", loggerLevelString,
		fmt.Sprintf("Comma-separated minimum per-logger level of messages to output, in the form of"+
			" [<logger>:]<level>,[<logger>:]<level>,... where logger components can be listed by running \"istioctl proxy-config log <pod-name[.namespace]>\""+
			"or referred from https://github.com/envoyproxy/envoy/blob/main/source/common/common/logger.h, and level can be one of %s", levelListString))

	return logCmd
}

func routeConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	routeConfigCmd := &cobra.Command{
		Use:   "route [<type>/]<name>[.<namespace>]",
		Short: "Retrieves route configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about route configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve summary about route configuration for a given pod from Envoy.
  istioctl proxy-config routes <pod-name[.namespace]>

  # Retrieve route summary for route 9080.
  istioctl proxy-config route <pod-name[.namespace]> --name 9080

  # Retrieve full route dump for route 9080
  istioctl proxy-config route <pod-name[.namespace]> --name 9080 -o json

  # Retrieve route summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config routes --file envoy-config.json
`,
		Aliases: []string{"routes", "r"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("route requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter *configdump.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, false, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}
			filter := configdump.RouteFilter{
				Name:    routeName,
				Verbose: verboseProxyConfig,
			}
			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintRouteSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintRouteDump(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	routeConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	routeConfigCmd.PersistentFlags().StringVar(&routeName, "name", "", "Filter listeners by route name field")
	routeConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	routeConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return routeConfigCmd
}

func endpointConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	endpointConfigCmd := &cobra.Command{
		Use:   "endpoint [<type>/]<name>[.<namespace>]",
		Short: "Retrieves endpoint configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about endpoint configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve full endpoint configuration for a given pod from Envoy.
  istioctl proxy-config endpoint <pod-name[.namespace]>

  # Retrieve endpoint summary for endpoint with port 9080.
  istioctl proxy-config endpoint <pod-name[.namespace]> --port 9080

  # Retrieve full endpoint with a address (172.17.0.2).
  istioctl proxy-config endpoint <pod-name[.namespace]> --address 172.17.0.2 -o json

  # Retrieve full endpoint with a cluster name (outbound|9411||zipkin.istio-system.svc.cluster.local).
  istioctl proxy-config endpoint <pod-name[.namespace]> --cluster "outbound|9411||zipkin.istio-system.svc.cluster.local" -o json
  # Retrieve full endpoint with the status (healthy).
  istioctl proxy-config endpoint <pod-name[.namespace]> --status healthy -ojson

  # Retrieve endpoint summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/clusters?format=json' > envoy-clusters.json
  istioctl proxy-config endpoints --file envoy-clusters.json
`,
		Aliases: []string{"endpoints", "ep"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("endpoints requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter *clusters.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodClustersWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
			} else {
				configWriter, err = setupFileClustersWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}

			filter := clusters.EndpointFilter{
				Address: address,
				Port:    uint32(port),
				Cluster: clusterName,
				Status:  status,
			}

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintEndpointsSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintEndpoints(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	endpointConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	endpointConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter endpoints by address field")
	endpointConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter endpoints by Port field")
	endpointConfigCmd.PersistentFlags().StringVar(&clusterName, "cluster", "", "Filter endpoints by cluster name field")
	endpointConfigCmd.PersistentFlags().StringVar(&status, "status", "", "Filter endpoints by status field")
	endpointConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return endpointConfigCmd
}

// edsConfigCmd is a command to dump EDS output. This differs from "endpoints" which pulls from /clusters.
// Notably, this shows metadata and locality, while clusters shows outlier health status
func edsConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	endpointConfigCmd := &cobra.Command{
		Use: "eds [<type>/]<name>[.<namespace>]",
		// Currently, we have an "endpoints" and "eds" command. While for simple use cases these are nearly identical, they give
		// pretty different outputs for the full JSON output. This makes it a useful command for developers, but may be overwhelming
		// for basic usage. For now, hide to avoid confusion.
		Hidden: true,
		Short:  "Retrieves endpoint configuration for the Envoy in the specified pod",
		Long:   `Retrieve information about endpoint configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve full endpoint configuration for a given pod from Envoy.
  istioctl proxy-config eds <pod-name[.namespace]>

  # Retrieve endpoint summary for endpoint with port 9080.
  istioctl proxy-config eds <pod-name[.namespace]> --port 9080

  # Retrieve full endpoint with a address (172.17.0.2).
  istioctl proxy-config eds <pod-name[.namespace]> --address 172.17.0.2 -o json

  # Retrieve full endpoint with a cluster name (outbound|9411||zipkin.istio-system.svc.cluster.local).
  istioctl proxy-config eds <pod-name[.namespace]> --cluster "outbound|9411||zipkin.istio-system.svc.cluster.local" -o json
  # Retrieve full endpoint with the status (healthy).
  istioctl proxy-config eds <pod-name[.namespace]> --status healthy -ojson

  # Retrieve endpoint summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump?include_eds=true' > envoy-config.json
  istioctl proxy-config eds --file envoy-config.json
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("eds requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter *configdump.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, true, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}

			filter := configdump.EndpointFilter{
				Address: address,
				Port:    uint32(port),
				Cluster: clusterName,
				Status:  status,
			}

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintEndpointsSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintEndpoints(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	endpointConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	endpointConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter endpoints by address field")
	endpointConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter endpoints by Port field")
	endpointConfigCmd.PersistentFlags().StringVar(&clusterName, "cluster", "", "Filter endpoints by cluster name field")
	endpointConfigCmd.PersistentFlags().StringVar(&status, "status", "", "Filter endpoints by status field")
	endpointConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return endpointConfigCmd
}

func bootstrapConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	// Shadow outputVariable since this command uses a different default value
	var outputFormat string

	bootstrapConfigCmd := &cobra.Command{
		Use:   "bootstrap [<type>/]<name>[.<namespace>]",
		Short: "Retrieves bootstrap configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about bootstrap configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve full bootstrap configuration for a given pod from Envoy.
  istioctl proxy-config bootstrap <pod-name[.namespace]>

  # Retrieve full bootstrap without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config bootstrap --file envoy-config.json

  # Show a human-readable Istio and Envoy version summary
  istioctl proxy-config bootstrap -o short
`,
		Aliases: []string{"b"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("bootstrap requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter *configdump.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, false, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintVersionSummary()
			case jsonOutput, yamlOutput:
				return configWriter.PrintBootstrapDump(outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	bootstrapConfigCmd.Flags().StringVarP(&outputFormat, "output", "o", jsonOutput, "Output format: one of json|yaml|short")
	bootstrapConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return bootstrapConfigCmd
}

func secretConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	secretConfigCmd := &cobra.Command{
		Use:   "secret [<type>/]<name>[.<namespace>]",
		Short: "Retrieves secret configuration for the Envoy in the specified pod",
		Long:  `Retrieve information about secret configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve full secret configuration for a given pod from Envoy.
  istioctl proxy-config secret <pod-name[.namespace]>

  # Retrieve full bootstrap without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config secret --file envoy-config.json`,
		Aliases: []string{"secrets", "s"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("secret requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var newWriter writer.ConfigDumpWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
				if ztunnelPod {
					newWriter, err = setupZtunnelConfigDumpWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
				} else {
					newWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, false, c.OutOrStdout())
				}
			} else {
				newWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
				if err != nil {
					envoyError := err
					newWriter, err = setupFileZtunnelConfigdumpWriter(configDumpFile, c.OutOrStdout())
					if err != nil {
						// failed to parse envoy and ztunnel formats
						log.Warnf("couldn't parse envoy secrets dump: %v", envoyError)
						log.Warnf("couldn't parse ztunnel secrets dump %v", err)
					}
				}
			}
			if err != nil {
				return err
			}
			switch outputFormat {
			case summaryOutput:
				return newWriter.PrintSecretSummary()
			case jsonOutput, yamlOutput:
				return newWriter.PrintSecretDump(outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	secretConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	secretConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")
	secretConfigCmd.Long += "\n\n" + istioctlutil.ExperimentalMsg
	return secretConfigCmd
}

func rootCACompareConfigCmd(ctx cli.Context) *cobra.Command {
	var podName1, podName2, podNamespace1, podNamespace2 string

	rootCACompareConfigCmd := &cobra.Command{
		Use:   "rootca-compare [pod/]<name-1>[.<namespace-1>] [pod/]<name-2>[.<namespace-2>]",
		Short: "Compare ROOTCA values for the two given pods",
		Long:  `Compare ROOTCA values for given 2 pods to check the connectivity between them.`,
		Example: `  # Compare ROOTCA values for given 2 pods to check the connectivity between them.
  istioctl proxy-config rootca-compare <pod-name-1[.namespace]> <pod-name-2[.namespace]>`,
		Aliases: []string{"rc"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("rootca-compare requires 2 pods as an argument")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter1, configWriter2 *configdump.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 2 {
				if podName1, podNamespace1, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter1, err = setupPodConfigdumpWriter(kubeClient, podName1, podNamespace1, false, c.OutOrStdout())
				if err != nil {
					return err
				}

				if podName2, podNamespace2, err = getPodName(ctx, args[1]); err != nil {
					return err
				}
				configWriter2, err = setupPodConfigdumpWriter(kubeClient, podName2, podNamespace2, false, c.OutOrStdout())
				if err != nil {
					return err
				}
			} else {
				c.Println(c.UsageString())
				return fmt.Errorf("rootca-compare requires 2 pods as an argument")
			}

			rootCA1, err1 := configWriter1.PrintPodRootCAFromDynamicSecretDump()
			if err1 != nil {
				return fmt.Errorf("error when retrieving ROOTCA of [%s]: %v", args[0], err1)
			}
			rootCA2, err2 := configWriter2.PrintPodRootCAFromDynamicSecretDump()
			if err2 != nil {
				return fmt.Errorf("error when retrieving ROOTCA of [%s]: %v", args[1], err2)
			}

			var returnErr error
			if rootCA1 == rootCA2 {
				report := fmt.Sprintf("Both [%s.%s] and [%s.%s] have the identical ROOTCA, theoretically the connectivity between them is available",
					podName1, podNamespace1, podName2, podNamespace2)
				c.Println(report)
				returnErr = nil
			} else {
				report := fmt.Sprintf("Both [%s.%s] and [%s.%s] have the non identical ROOTCA, theoretically the connectivity between them is unavailable",
					podName1, podNamespace1, podName2, podNamespace2)
				returnErr = fmt.Errorf(report)
			}
			return returnErr
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	rootCACompareConfigCmd.Long += "\n\n" + istioctlutil.ExperimentalMsg
	return rootCACompareConfigCmd
}

func ProxyConfig(ctx cli.Context) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "proxy-config",
		Short: "Retrieve information about proxy configuration from Envoy [kube only]",
		Long:  `A group of commands used to retrieve information about proxy configuration from the Envoy config dump`,
		Example: `  # Retrieve information about proxy configuration from an Envoy instance.
  istioctl proxy-config <clusters|listeners|routes|endpoints|bootstrap|log|secret> <pod-name[.namespace]>`,
		Aliases: []string{"pc"},
	}

	configCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	configCmd.PersistentFlags().IntVar(&proxyAdminPort, "proxy-admin-port", defaultProxyAdminPort, "Envoy proxy admin port")

	configCmd.AddCommand(clusterConfigCmd(ctx))
	configCmd.AddCommand(allConfigCmd(ctx))
	configCmd.AddCommand(listenerConfigCmd(ctx))
	configCmd.AddCommand(logCmd(ctx))
	configCmd.AddCommand(routeConfigCmd(ctx))
	configCmd.AddCommand(bootstrapConfigCmd(ctx))
	configCmd.AddCommand(endpointConfigCmd(ctx))
	configCmd.AddCommand(edsConfigCmd(ctx))
	configCmd.AddCommand(secretConfigCmd(ctx))
	configCmd.AddCommand(rootCACompareConfigCmd(ctx))
	configCmd.AddCommand(ecdsConfigCmd(ctx))
	configCmd.AddCommand(workloadConfigCmd(ctx))

	return configCmd
}

func getPodNames(ctx cli.Context, podflag, ns string) ([]string, string, error) {
	podNames, ns, err := ctx.InferPodsFromTypedResource(podflag, ns)
	if err != nil {
		log.Errorf("pods lookup failed")
		return []string{}, "", err
	}
	return podNames, ns, nil
}

func getPodName(ctx cli.Context, podflag string) (string, string, error) {
	return getPodNameWithNamespace(ctx, podflag, ctx.Namespace())
}

func getPodNameWithNamespace(ctx cli.Context, podflag, ns string) (string, string, error) {
	var podName, podNamespace string
	podName, podNamespace, err := ctx.InferPodInfoFromTypedResource(podflag, ns)
	if err != nil {
		return "", "", err
	}
	return podName, podNamespace, nil
}

// getComponentPodName returns the pod name and namespace of the Istio component
func getComponentPodName(ctx cli.Context, podflag string) (string, string, error) {
	return getPodNameWithNamespace(ctx, podflag, ctx.IstioNamespace())
}

func getPodNameBySelector(ctx cli.Context, kubeClient kube.CLIClient, labelSelector string) ([]string, string, error) {
	var (
		podNames []string
		ns       string
	)
	pl, err := kubeClient.PodsForSelector(context.TODO(), ctx.NamespaceOrDefault(ctx.Namespace()), labelSelector)
	if err != nil {
		return nil, "", fmt.Errorf("not able to locate pod with selector %s: %v", labelSelector, err)
	}
	if len(pl.Items) < 1 {
		return nil, "", errors.New("no pods found")
	}
	for _, pod := range pl.Items {
		podNames = append(podNames, pod.Name)
	}
	ns = pl.Items[0].Namespace
	return podNames, ns, nil
}

func ecdsConfigCmd(ctx cli.Context) *cobra.Command {
	var podName, podNamespace string

	ecdsConfigCmd := &cobra.Command{
		Use:     "ecds [<type>/]<name>[.<namespace>]",
		Aliases: []string{"ec"},
		Short:   "Retrieves typed extension configuration for the Envoy in the specified pod",
		Long:    `Retrieve information about typed extension configuration for the Envoy instance in the specified pod.`,
		Example: `  # Retrieve full typed extension configuration for a given pod from Envoy.
  istioctl proxy-config ecds <pod-name[.namespace]>

  # Retrieve endpoint summary without using Kubernetes API
  ssh <user@hostname> 'curl localhost:15000/config_dump' > envoy-config.json
  istioctl proxy-config ecds --file envoy-config.json
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("ecds requires pod name or --file parameter")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configWriter *configdump.ConfigWriter
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(ctx, args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(kubeClient, podName, podNamespace, true, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintEcdsSummary()
			case jsonOutput, yamlOutput:
				return configWriter.PrintEcds(outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	ecdsConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	ecdsConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "", "Envoy config dump JSON file")

	return ecdsConfigCmd
}
