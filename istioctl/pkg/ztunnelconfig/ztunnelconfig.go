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

package ztunnelconfig

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

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	ambientutil "istio.io/istio/istioctl/pkg/util/ambient"
	ztunnelDump "istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

const (
	jsonOutput    = "json"
	yamlOutput    = "yaml"
	summaryOutput = "short"

	defaultProxyAdminPort = 15000
)

var (
	workloadsNamespace string
	verboseProxyConfig bool

	address string

	labelSelector = ""
)

type commonFlags struct {
	// output format (json, yaml or short)
	outputFormat string

	proxyAdminPort int

	configDumpFile string

	node string
}

func (c commonFlags) attach(cmd *cobra.Command) {
	cmd.PersistentFlags().IntVar(&c.proxyAdminPort, "proxy-admin-port", defaultProxyAdminPort, "Ztunnel proxy admin port")
	cmd.PersistentFlags().StringVarP(&c.outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	cmd.PersistentFlags().StringVar(&c.node, "node", "", "Filter workloads by node field")
	cmd.PersistentFlags().StringVarP(&c.configDumpFile, "file", "f", "",
		"Ztunnel config dump JSON file")
}
func (c commonFlags) validateArgs(cmd *cobra.Command, args []string) error {
	set := 0
	if c.configDumpFile != "" {
		set++
	}
	if len(args) == 1 {
		set++
	}
	if c.node != "" {
		set++
	}
	if set != 1 {
		cmd.Println(cmd.UsageString())
		return fmt.Errorf("exactly one of --file, --node, or pod name must be passed")
	}
	return nil
}

func ZtunnelConfig(ctx cli.Context) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "ztunnel-config",
		Short: "Update or retrieve current Ztunnel configuration.",
		Long:  "A group of commands used to update or retrieve Ztunnel configuration from a Ztunnel instance.",
		Example: `  # Retrieve summary about workload configuration for a given Ztunnel instance.
	istioctl ztunnel-config <workload> <ztunnel-name[.namespace]>`,
		Aliases: []string{"zc"},
	}

	configCmd.AddCommand(logCmd(ctx))
	configCmd.AddCommand(workloadConfigCmd(ctx))
	configCmd.AddCommand(certificatesConfigCmd(ctx))

	return configCmd
}

type Command struct {
	Name string
}

func certificatesConfigCmd(ctx cli.Context) *cobra.Command {
	var common commonFlags
	cmd := &cobra.Command{
		Use:   "certificates [<type>/]<name>[.<namespace>]",
		Short: "Retrieves certificate for the specified Ztunnel pod.",
		Long:  `Retrieve information about certificates for the Ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a given Ztunnel instance.
  istioctl ztunnel-config certificates <ztunnel-name[.namespace]>

  # Retrieve full certificate dump of workloads for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> -o json
`,
		Aliases: []string{"workloads", "w"},
		Args:    common.validateArgs,
		RunE:  run(ctx, func(cw *ztunnelDump.ConfigWriter) error {
			switch common.outputFormat {
			case summaryOutput:
			return cw.PrintSecretSummary()
			case jsonOutput, yamlOutput:
			return cw.PrintSecretDump(common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ValidPodsNameArgs(cmd, ctx, args, toComplete)
		},
	}

	common.attach(cmd)

	return cmd
}

func commonArgs(cmd *cobra.Command, args []string) error {
	if (len(args) == 1) != (configDumpFile == "") {
		cmd.Println(cmd.UsageString())
		return fmt.Errorf("requires pod name or --file parameter")
	}
	return nil
}

func run(ctx cli.Context, f func(cw *ztunnelDump.ConfigWriter) error) func(c *cobra.Command, args []string) error {
	return func(c *cobra.Command, args []string) error {
		var podName, podNamespace string
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
				return fmt.Errorf("workloads command is only supported by Ztunnel proxies: %v", podName)
			}
			configWriter, err = setupZtunnelConfigDumpWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
		} else {
			configWriter, err = setupFileZtunnelConfigdumpWriter(configDumpFile, c.OutOrStdout())
		}
		if err != nil {
			return err
		}
		return f(configWriter)
	}
}

func workloadConfigCmd(ctx cli.Context) *cobra.Command {
	workloadConfigCmd := &cobra.Command{
		Use:   "workload [<type>/]<name>[.<namespace>]",
		Short: "Retrieves workload configuration for the specified Ztunnel pod.",
		Long:  `Retrieve information about workload configuration for the Ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]>

  # Retrieve summary of workloads on node XXXX for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --node ambient-worker

  # Retrieve full workload dump of workloads with address XXXX for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --address 0.0.0.0 -o json

  # Retrieve Ztunnel config dump separately and inspect from file.
  kubectl exec -it $ZTUNNEL -n istio-system -- curl localhost:15000/config_dump > ztunnel-config.json
  istioctl ztunnel-config workloads --file ztunnel-config.json

  # Retrieve workload summary for a specific namespace
  istioctl ztunnel-config workloads <ztunnel-name[.namespace]> --workloads-namespace foo
`,
		Aliases: []string{"certs"},
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) != (configDumpFile == "") {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("workload requires pod name or --file parameter")
			}
			return nil
		},
		RunE: run(ctx, func(cw *ztunnelDump.ConfigWriter) error {
			filter := ztunnelDump.WorkloadFilter{
				Namespace: workloadsNamespace,
				Address:   address,
				Node:      node,
				Verbose:   verboseProxyConfig,
			}
			_ = filter

			switch outputFormat {
			case summaryOutput:
				return cw.PrintSecretSummary()
			case jsonOutput, yamlOutput:
				return cw.PrintSecretDump(outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	workloadConfigCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	workloadConfigCmd.PersistentFlags().StringVar(&address, "address", "", "Filter workloads by address field")
	workloadConfigCmd.PersistentFlags().StringVar(&node, "node", "", "Filter workloads by node field")
	workloadConfigCmd.PersistentFlags().BoolVar(&verboseProxyConfig, "verbose", true, "Output more information")
	workloadConfigCmd.PersistentFlags().StringVarP(&configDumpFile, "file", "f", "",
		"Ztunnel config dump JSON file")
	workloadConfigCmd.PersistentFlags().StringVar(&workloadsNamespace, "workload-namespace", "",
		"Filter workloads by namespace field")

	return workloadConfigCmd
}

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

func logCmd(ctx cli.Context) *cobra.Command {
	var podNamespace string
	var podNames []string

	logCmd := &cobra.Command{
		Use:   "log [<type>/]<name>[.<namespace>]",
		Short: "Retrieves logging levels of the Ztunnel instance in the specified pod.",
		Long:  "Retrieve information about logging levels of the Ztunnel instance in the specified pod, and update optionally.",
		Example: `  # Retrieve information about logging levels for a given pod from Ztunnel.
  istioctl ztunnel-config log <pod-name[.namespace]>

  # Update levels of the all loggers
  istioctl ztunnel-config log <pod-name[.namespace]> --level none

  # Update levels of the specified loggers.
  istioctl ztunnel-config log <pod-name[.namespace]> --level http:debug,redis:debug

  # Reset levels of all the loggers to default value (warning).
  istioctl ztunnel-config log <pod-name[.namespace]> -r
`,
		Aliases: []string{"o"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("--level needs a value")
			}
			if reset && loggerLevelString != "" {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("--level cannot be combined with --reset")
			}
			if outputFormat != "" && outputFormat != summaryOutput {
				return fmt.Errorf("--output is not applicable for this command")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
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
				_, err = setupEnvoyLogConfig(kubeClient, "", pod, podNamespace)
				if err != nil {
					return err
				}
			}

			destLoggerLevels := map[string]Level{}
			if reset {
				log.Warn("logLevel reset; using default value \"info\" for Ztunnel")
				loggerLevelString = "info"
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
						// TODO validate ztunnel logger name when available: https://github.com/istio/ztunnel/issues/426
						level, ok := stringToLevel[loggerLevel[1]]
						if !ok {
							return fmt.Errorf("unrecognized logging level: %v", loggerLevel[1])
						}
						destLoggerLevels[loggerLevel[0]] = level
					}
				}
			}

			var errs *multierror.Error
			for _, podName := range podNames {
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
			}
			if err := multierror.Flatten(errs.ErrorOrNil()); err != nil {
				return err
			}
			return nil
		},
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
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
			" [<logger>:]<level>,[<logger>:]<level>,... or <level> to change all active loggers, "+
			"where logger components can be listed by running \"istioctl ztunnel-config log <pod-name[.namespace]>\""+
			", and level can be one of %s", levelListString))
	return logCmd
}

func setupEnvoyLogConfig(kubeClient kube.CLIClient, param, podName, podNamespace string) (string, error) {
	path := "logging"
	if param != "" {
		path = path + "?" + param
	}
	result, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "POST", path, proxyAdminPort)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Ztunnel: %v", err)
	}
	return string(result), nil
}

func getPodNames(ctx cli.Context, podflag, ns string) ([]string, string, error) {
	podNames, ns, err := ctx.InferPodsFromTypedResource(podflag, ns)
	if err != nil {
		log.Errorf("pods lookup failed")
		return []string{}, "", err
	}
	return podNames, ns, nil
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

// getComponentPodName returns the pod name and namespace of the Istio component
func getComponentPodName(ctx cli.Context, podflag string) (string, string, error) {
	return getPodNameWithNamespace(ctx, podflag, ctx.IstioNamespace())
}

func getPodNameWithNamespace(ctx cli.Context, podflag, ns string) (string, string, error) {
	var podName, podNamespace string
	podName, podNamespace, err := ctx.InferPodInfoFromTypedResource(podflag, ns)
	if err != nil {
		return "", "", err
	}
	return podName, podNamespace, nil
}

func setupZtunnelConfigDumpWriter(kubeClient kube.CLIClient, podName, podNamespace string, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	debug, err := extractZtunnelConfigDump(kubeClient, podName, podNamespace)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpZtunnelConfigWriter(debug, out)
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

func extractZtunnelConfigDump(kubeClient kube.CLIClient, podName, podNamespace string) ([]byte, error) {
	path := "config_dump"
	debug, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "GET", path, 15000)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command on %s.%s Ztunnel: %v", podName, podNamespace, err)
	}
	return debug, err
}

func setupConfigdumpZtunnelConfigWriter(debug []byte, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	cw := &ztunnelDump.ConfigWriter{Stdout: out}
	err := cw.Prime(debug)
	if err != nil {
		return nil, err
	}
	return cw, nil
}

func setupFileZtunnelConfigdumpWriter(filename string, out io.Writer) (*ztunnelDump.ConfigWriter, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}
	return setupConfigdumpZtunnelConfigWriter(data, out)
}
