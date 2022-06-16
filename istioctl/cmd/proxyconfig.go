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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/istioctl/pkg/writer/envoy/clusters"
	"istio.io/istio/istioctl/pkg/writer/envoy/configdump"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

const (
	jsonOutput       = "json"
	yamlOutput       = "yaml"
	summaryOutput    = "short"
	prometheusOutput = "prom"
)

var (
	fqdn, direction, subset string
	port                    int
	verboseProxyConfig      bool

	address, listenerType, statsType string

	routeName string

	clusterName, status string

	// output format (yaml or short)
	outputFormat string
)

// Level is an enumeration of all supported log levels.
type Level int

const (
	defaultLoggerName  = "level"
	defaultOutputLevel = WarningLevel
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

// existing sorted active loggers
var activeLoggers = []string{
	"admin",
	"aws",
	"assert",
	"backtrace",
	"client",
	"config",
	"connection",
	"conn_handler", // Added through https://github.com/envoyproxy/envoy/pull/8263
	"dubbo",
	"file",
	"filter",
	"forward_proxy",
	"grpc",
	"hc",
	"health_checker",
	"http",
	"http2",
	"hystrix",
	"init",
	"io",
	"jwt",
	"kafka",
	"lua",
	"main",
	"misc",
	"mongo",
	"quic",
	"pool",
	"rbac",
	"redis",
	"router",
	"runtime",
	"stats",
	"secret",
	"tap",
	"testing",
	"thrift",
	"tracing",
	"upstream",
	"udp",
	"wasm",
}

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
	"error":    ErrorLevel,
	"critical": CriticalLevel,
	"off":      OffLevel,
}

var (
	loggerLevelString = ""
	reset             = false
)

func extractConfigDump(podName, podNamespace string, eds bool) ([]byte, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %v", err)
	}
	path := "config_dump"
	if eds {
		path += "?include_eds=true"
	}
	debug, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "GET", path)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on %s.%s sidecar: %v", podName, podNamespace, err)
	}
	return debug, err
}

func setupPodConfigdumpWriter(podName, podNamespace string, includeEds bool, out io.Writer) (*configdump.ConfigWriter, error) {
	debug, err := extractConfigDump(podName, podNamespace, includeEds)
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

func setupFileConfigdumpWriter(filename string, out io.Writer) (*configdump.ConfigWriter, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpEnvoyConfigWriter(data, out)
}

func setupConfigdumpEnvoyConfigWriter(debug []byte, out io.Writer) (*configdump.ConfigWriter, error) {
	cw := &configdump.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

func setupEnvoyClusterStatsConfig(podName, podNamespace string, outputFormat string) (string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %v", err)
	}
	path := "clusters"
	if outputFormat == jsonOutput || outputFormat == yamlOutput {
		// for yaml output we will convert the json to yaml when printed
		path += "?format=json"
	}
	result, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "GET", path)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func setupEnvoyServerStatsConfig(podName, podNamespace string, outputFormat string) (string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %v", err)
	}
	path := "stats"
	if outputFormat == jsonOutput || outputFormat == yamlOutput {
		// for yaml output we will convert the json to yaml when printed
		path += "?format=json"
	}
	if outputFormat == prometheusOutput {
		path += "/prometheus"
	}
	result, err := kubeClient.EnvoyDo(context.Background(), podName, podNamespace, "GET", path)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func setupEnvoyLogConfig(param, podName, podNamespace string) (string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %v", err)
	}
	path := "logging"
	if param != "" {
		path = path + "?" + param
	}
	result, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "POST", path)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return string(result), nil
}

func getLogLevelFromConfigMap() (string, error) {
	valuesConfig, err := getValuesFromConfigMap(kubeconfig, "")
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

func setupPodClustersWriter(podName, podNamespace string, out io.Writer) (*clusters.ConfigWriter, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %v", err)
	}
	path := "clusters?format=json"
	debug, err := kubeClient.EnvoyDo(context.TODO(), podName, podNamespace, "GET", path)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on Envoy: %v", err)
	}
	return setupClustersEnvoyConfigWriter(debug, out)
}

func setupFileClustersWriter(filename string, out io.Writer) (*clusters.ConfigWriter, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Errorf("failed to close %s: %s", filename, err)
		}
	}()
	data, err := io.ReadAll(file)
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

func clusterConfigCmd() *cobra.Command {
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
			var configWriter *configdump.ConfigWriter
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, false, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
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

func allConfigCmd() *cobra.Command {
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
			switch outputFormat {
			case jsonOutput, yamlOutput:
				var dump []byte
				var err error
				if len(args) == 1 {
					podName, podNamespace, err := getPodName(args[0])
					if err != nil {
						return err
					}
					dump, err = extractConfigDump(podName, podNamespace, false)
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
					podName, podNamespace, err := getPodName(args[0])
					if err != nil {
						return err
					}
					configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, true, c.OutOrStdout())
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
				)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
			return nil
		},
		ValidArgsFunction: validPodsNameArgs,
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

func listenerConfigCmd() *cobra.Command {
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
			var configWriter *configdump.ConfigWriter
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, false, c.OutOrStdout())
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

			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintListenerSummary(filter)
			case jsonOutput, yamlOutput:
				return configWriter.PrintListenerDump(filter, outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: validPodsNameArgs,
	}

	listenerConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	listenerConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter listeners by address field")
	listenerConfigCmd.PersistentFlags().StringVar(&listenerType, "type", "", "Filter listeners by type field")
	listenerConfigCmd.PersistentFlags().IntVar(&port, "port", 0, "Filter listeners by Port field")
	listenerConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	listenerConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return listenerConfigCmd
}

func statsConfigCmd() *cobra.Command {
	var podName, podNamespace string

	statsConfigCmd := &cobra.Command{
		Use:   "envoy-stats [<type>/]<name>[.<namespace>]",
		Short: "Retrieves Envoy metrics in the specified pod",
		Long:  `Retrieve Envoy emitted metrics for the specified pod.`,
		Example: `  # Retrieve Envoy emitted metrics for the specified pod.
  istioctl experimental envoy-stats <pod-name[.namespace]>

  # Retrieve Envoy server metrics in prometheus format
  istioctl experimental envoy-stats <pod-name[.namespace]> --output prom

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
			var err error

			if podName, podNamespace, err = getPodName(args[0]); err != nil {
				return err
			}
			if statsType == "" || statsType == "server" {
				stats, err = setupEnvoyServerStatsConfig(podName, podNamespace, outputFormat)
				if err != nil {
					return err
				}
			} else if statsType == "cluster" || statsType == "clusters" {
				stats, err = setupEnvoyClusterStatsConfig(podName, podNamespace, outputFormat)
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
		ValidArgsFunction: validPodsNameArgs,
	}
	statsConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|prom")
	statsConfigCmd.PersistentFlags().StringVarP(&statsType, "type", "t", "server", "Where to grab the stats: one of server|clusters")

	return statsConfigCmd
}

func logCmd() *cobra.Command {
	var podName, podNamespace string
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
			var err error
			var loggerNames []string
			if labelSelector != "" {
				if podNames, podNamespace, err = getPodNameBySelector(labelSelector); err != nil {
					return err
				}
				for _, pod := range podNames {
					name, err = setupEnvoyLogConfig("", pod, podNamespace)
					loggerNames = append(loggerNames, name)
				}
				if err != nil {
					return err
				}
				if len(podNames) > 0 {
					podName = podNames[0]
				}
			} else {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				name, err := setupEnvoyLogConfig("", podName, podNamespace)
				loggerNames = append(loggerNames, name)
				if err != nil {
					return err
				}
			}

			destLoggerLevels := map[string]Level{}
			if reset {
				// reset logging level to `defaultOutputLevel`, and ignore the `level` option
				levelString, _ := getLogLevelFromConfigMap()
				level, ok := stringToLevel[levelString]
				if ok {
					destLoggerLevels[defaultLoggerName] = level
				} else {
					log.Warnf("unable to get logLevel from ConfigMap istio-sidecar-injector, using default value: %v",
						levelToString[defaultOutputLevel])
					destLoggerLevels[defaultLoggerName] = defaultOutputLevel
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
						loggerLevel := regexp.MustCompile(`[:=]`).Split(ol, 2)
						for _, logName := range loggerNames {
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
			if len(destLoggerLevels) == 0 {
				resp, err = setupEnvoyLogConfig("", podName, podNamespace)
			} else {
				if ll, ok := destLoggerLevels[defaultLoggerName]; ok {
					// update levels of all loggers first
					resp, err = setupEnvoyLogConfig(defaultLoggerName+"="+levelToString[ll], podName, podNamespace)
					delete(destLoggerLevels, defaultLoggerName)
				}
				for lg, ll := range destLoggerLevels {
					resp, err = setupEnvoyLogConfig(lg+"="+levelToString[ll], podName, podNamespace)
				}
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(c.OutOrStdout(), resp)
			return nil
		},
		ValidArgsFunction: validPodsNameArgs,
	}

	levelListString := fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s]",
		levelToString[TraceLevel],
		levelToString[DebugLevel],
		levelToString[InfoLevel],
		levelToString[WarningLevel],
		levelToString[ErrorLevel],
		levelToString[CriticalLevel],
		levelToString[OffLevel])
	s := strings.Join(activeLoggers, ", ")

	logCmd.PersistentFlags().BoolVarP(&reset, "reset", "r", reset, "Reset levels to default value (warning).")
	logCmd.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "Label selector")
	logCmd.PersistentFlags().StringVar(&loggerLevelString, "level", loggerLevelString,
		fmt.Sprintf("Comma-separated minimum per-logger level of messages to output, in the form of"+
			" [<logger>:]<level>,[<logger>:]<level>,... where logger can be one of %s and level can be one of %s",
			s, levelListString))

	return logCmd
}

func routeConfigCmd() *cobra.Command {
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
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, false, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
	}

	routeConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	routeConfigCmd.PersistentFlags().StringVar(&routeName, "name", "", "Filter listeners by route name field")
	routeConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	routeConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return routeConfigCmd
}

func endpointConfigCmd() *cobra.Command {
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
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodClustersWriter(podName, podNamespace, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
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
func edsConfigCmd() *cobra.Command {
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
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, true, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
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

func bootstrapConfigCmd() *cobra.Command {
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
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, false, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
	}

	bootstrapConfigCmd.Flags().StringVarP(&outputFormat, "output", "o", jsonOutput, "Output format: one of json|yaml|short")
	bootstrapConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")

	return bootstrapConfigCmd
}

func secretConfigCmd() *cobra.Command {
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
			var configWriter *configdump.ConfigWriter
			var err error
			if len(args) == 1 {
				if podName, podNamespace, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter, err = setupPodConfigdumpWriter(podName, podNamespace, false, c.OutOrStdout())
			} else {
				configWriter, err = setupFileConfigdumpWriter(configDumpFile, c.OutOrStdout())
			}
			if err != nil {
				return err
			}
			switch outputFormat {
			case summaryOutput:
				return configWriter.PrintSecretSummary()
			case jsonOutput, yamlOutput:
				return configWriter.PrintSecretDump(outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		},
		ValidArgsFunction: validPodsNameArgs,
	}

	secretConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	secretConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Envoy config dump JSON file")
	secretConfigCmd.Long += "\n\n" + ExperimentalMsg
	return secretConfigCmd
}

func rootCACompareConfigCmd() *cobra.Command {
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
			var err error
			if len(args) == 2 {
				if podName1, podNamespace1, err = getPodName(args[0]); err != nil {
					return err
				}
				configWriter1, err = setupPodConfigdumpWriter(podName1, podNamespace1, false, c.OutOrStdout())
				if err != nil {
					return err
				}

				if podName2, podNamespace2, err = getPodName(args[1]); err != nil {
					return err
				}
				configWriter2, err = setupPodConfigdumpWriter(podName2, podNamespace2, false, c.OutOrStdout())
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
		ValidArgsFunction: validPodsNameArgs,
	}

	rootCACompareConfigCmd.Long += "\n\n" + ExperimentalMsg
	return rootCACompareConfigCmd
}

func proxyConfig() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "proxy-config",
		Short: "Retrieve information about proxy configuration from Envoy [kube only]",
		Long:  `A group of commands used to retrieve information about proxy configuration from the Envoy config dump`,
		Example: `  # Retrieve information about proxy configuration from an Envoy instance.
  istioctl proxy-config <clusters|listeners|routes|endpoints|bootstrap|log|secret> <pod-name[.namespace]>`,
		Aliases: []string{"pc"},
	}

	configCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")

	configCmd.AddCommand(clusterConfigCmd())
	configCmd.AddCommand(allConfigCmd())
	configCmd.AddCommand(listenerConfigCmd())
	configCmd.AddCommand(logCmd())
	configCmd.AddCommand(routeConfigCmd())
	configCmd.AddCommand(bootstrapConfigCmd())
	configCmd.AddCommand(endpointConfigCmd())
	configCmd.AddCommand(edsConfigCmd())
	configCmd.AddCommand(secretConfigCmd())
	configCmd.AddCommand(rootCACompareConfigCmd())

	return configCmd
}

func getPodName(podflag string) (string, string, error) {
	kubeClient, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return "", "", fmt.Errorf("failed to create k8s client: %w", err)
	}
	var podName, ns string
	podName, ns, err = handlers.InferPodInfoFromTypedResource(podflag,
		handlers.HandleNamespace(namespace, defaultNamespace),
		kubeClient.UtilFactory())
	if err != nil {
		return "", "", err
	}
	return podName, ns, nil
}

func getPodNameBySelector(labelSelector string) ([]string, string, error) {
	var (
		podNames []string
		ns       string
	)
	client, err := kubeClient(kubeconfig, configContext)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create k8s client: %w", err)
	}
	pl, err := client.PodsForSelector(context.TODO(), handlers.HandleNamespace(namespace, defaultNamespace), labelSelector)
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
