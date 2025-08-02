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
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/util/podutils"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	ambientutil "istio.io/istio/istioctl/pkg/util/ambient"
	ztunnelDump "istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/slices"
)

const (
	jsonOutput    = "json"
	yamlOutput    = "yaml"
	summaryOutput = "short"
)

func ZtunnelConfig(ctx cli.Context) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "ztunnel-config",
		Short: "Update or retrieve current Ztunnel configuration.",
		Long:  "A group of commands used to update or retrieve Ztunnel configuration from a Ztunnel instance.",
		Example: `  # Retrieve summary about workload configuration
  istioctl ztunnel-config workload

  # Retrieve summary about certificates
  istioctl ztunnel-config certificates`,
		Aliases: []string{"zc"},
	}

	configCmd.AddCommand(logCmd(ctx))
	configCmd.AddCommand(workloadConfigCmd(ctx))
	configCmd.AddCommand(certificatesConfigCmd(ctx))
	configCmd.AddCommand(servicesCmd(ctx))
	configCmd.AddCommand(policiesCmd(ctx))
	configCmd.AddCommand(allCmd(ctx))
	configCmd.AddCommand(connectionsCmd(ctx))

	return configCmd
}

type Command struct {
	Name string
}

func certificatesConfigCmd(ctx cli.Context) *cobra.Command {
	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "certificate",
		Short: "Retrieves certificate for the specified Ztunnel pod.",
		Long:  `Retrieve information about certificates for the Ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a randomly chosen ztunnel.
  istioctl ztunnel-config certificates

  # Retrieve full certificate dump of workloads for a given Ztunnel instance.
  istioctl ztunnel-config certificates <ztunnel-name[.namespace]> -o json
`,
		Aliases: []string{"certificates", "certs", "cert"},
		Args:    common.validateArgs,
		RunE: runConfigDump(ctx, common, true, func(cw *ztunnelDump.ConfigWriter) error {
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintSecretSummary()
			case jsonOutput, yamlOutput:
				return cw.PrintSecretDump(common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)

	return cmd
}

func servicesCmd(ctx cli.Context) *cobra.Command {
	var serviceNamespace string
	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Retrieves services for the specified Ztunnel pod.",
		Long:  `Retrieve information about services for the Ztunnel instance.`,
		Example: `  # Retrieve summary about services configuration for a randomly chosen ztunnel.
  istioctl ztunnel-config services

  # Retrieve full services dump of workloads for a given Ztunnel instance.
  istioctl ztunnel-config services <ztunnel-name[.namespace]> -o json
`,
		Aliases: []string{"services", "s", "svc"},
		Args:    common.validateArgs,
		RunE: runConfigDump(ctx, common, false, func(cw *ztunnelDump.ConfigWriter) error {
			filter := ztunnelDump.ServiceFilter{
				Namespace: serviceNamespace,
			}
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintServiceSummary(filter)
			case jsonOutput, yamlOutput:
				return cw.PrintServiceDump(filter, common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)
	cmd.PersistentFlags().StringVar(&serviceNamespace, "service-namespace", "",
		"Filter services by namespace field")

	return cmd
}

func policiesCmd(ctx cli.Context) *cobra.Command {
	var policyNamespace string
	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Retrieves policies for the specified Ztunnel pod.",
		Long:  `Retrieve information about policies for the Ztunnel instance.`,
		Example: `  # Retrieve summary about policy configuration for a randomly chosen ztunnel.
  istioctl ztunnel-config policies

  # Retrieve full policy dump of workloads for a given Ztunnel instance.
  istioctl ztunnel-config policies <ztunnel-name[.namespace]> -o json
`,
		Aliases: []string{"policies", "p", "pol"},
		Args:    common.validateArgs,
		RunE: runConfigDump(ctx, common, false, func(cw *ztunnelDump.ConfigWriter) error {
			filter := ztunnelDump.PolicyFilter{
				Namespace: policyNamespace,
			}
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintPolicySummary(filter)
			case jsonOutput, yamlOutput:
				return cw.PrintPolicyDump(filter, common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)
	cmd.PersistentFlags().StringVar(&policyNamespace, "policy-namespace", "",
		"Filter policies by namespace field")

	return cmd
}

func allCmd(ctx cli.Context) *cobra.Command {
	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Retrieves all configuration for the specified Ztunnel pod.",
		Long:  `Retrieve information about all configuration for the Ztunnel instance.`,
		Example: `  # Retrieve summary about all configuration for a randomly chosen ztunnel.
  istioctl ztunnel-config all

  # Retrieve full configuration dump of workloads for a given Ztunnel instance.
  istioctl ztunnel-config all <ztunnel-name[.namespace]> -o json
`,
		Args: common.validateArgs,
		RunE: runConfigDump(ctx, common, false, func(cw *ztunnelDump.ConfigWriter) error {
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintFullSummary()
			case jsonOutput, yamlOutput:
				return cw.PrintFullDump(common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)

	return cmd
}

func workloadConfigCmd(ctx cli.Context) *cobra.Command {
	var workloadsNamespace string
	var workloadNode string

	var address string

	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "workload [<type>/]<name>[.<namespace>]",
		Short: "Retrieves workload configuration for the specified Ztunnel pod.",
		Long:  `Retrieve information about workload configuration for the Ztunnel instance.`,
		Example: `  # Retrieve summary about workload configuration for a randomly chosen ztunnel.
  istioctl ztunnel-config workload

  # Retrieve summary of workloads on node XXXX for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --node ambient-worker

  # Retrieve full workload dump of workloads with address XXXX for a given Ztunnel instance.
  istioctl ztunnel-config workload <ztunnel-name[.namespace]> --address 0.0.0.0 -o json

  # Retrieve Ztunnel config dump separately and inspect from file.
  kubectl exec -it $ZTUNNEL -n istio-system -- curl localhost:15000/config_dump > ztunnel-config.json
  istioctl ztunnel-config workloads --file ztunnel-config.json

  # Retrieve workload summary for a specific namespace
  istioctl ztunnel-config workloads <ztunnel-name[.namespace]> --workload-namespace foo
`,
		Aliases: []string{"w", "workloads"},
		Args:    common.validateArgs,
		RunE: runConfigDump(ctx, common, false, func(cw *ztunnelDump.ConfigWriter) error {
			filter := ztunnelDump.WorkloadFilter{
				Namespace: workloadsNamespace,
				Address:   address,
				Node:      workloadNode,
			}
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintWorkloadSummary(filter)
			case jsonOutput, yamlOutput:
				return cw.PrintWorkloadDump(filter, common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)
	cmd.PersistentFlags().StringVar(&address, "address", "", "Filter workloads by address field")
	cmd.PersistentFlags().StringVar(&workloadsNamespace, "workload-namespace", "",
		"Filter workloads by namespace field")
	cmd.PersistentFlags().StringVar(&workloadNode, "workload-node", "",
		"Filter workloads by node")

	return cmd
}

func connectionsCmd(ctx cli.Context) *cobra.Command {
	var workloadsNamespace string
	var direction string
	var raw bool

	common := new(commonFlags)
	cmd := &cobra.Command{
		Use:   "connections [<type>/]<name>[.<namespace>]",
		Short: "Retrieves connections for the specified Ztunnel pod.",
		Long:  `Retrieve information about connections for the Ztunnel instance.`,
		Example: `  # Retrieve summary about connections for the ztunnel on a specific node.
  istioctl ztunnel-config connections --node ambient-worker

  # Retrieve summary of connections for a given Ztunnel instance.
  istioctl ztunnel-config connections <ztunnel-name[.namespace]>
`,
		Aliases: []string{"cons"},
		Args:    common.validateArgs,
		RunE: runConfigDump(ctx, common, true, func(cw *ztunnelDump.ConfigWriter) error {
			filter := ztunnelDump.ConnectionsFilter{
				Namespace: workloadsNamespace,
				Direction: direction,
				Raw:       raw,
			}
			switch common.outputFormat {
			case summaryOutput:
				return cw.PrintConnectionsSummary(filter)
			case jsonOutput, yamlOutput:
				return cw.PrintConnectionsDump(filter, common.outputFormat)
			default:
				return fmt.Errorf("output format %q not supported", common.outputFormat)
			}
		}),
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}

	common.attach(cmd)
	cmd.PersistentFlags().StringVar(&direction, "direction", "", "Filter workloads by direction (inbound or outbound)")
	cmd.PersistentFlags().BoolVar(&raw, "raw", false, "If set, show IP addresses instead of names")
	cmd.PersistentFlags().StringVar(&workloadsNamespace, "workload-namespace", "",
		"Filter workloads by namespace field")

	return cmd
}

// Level is an enumeration of all supported log levels.
type Level int

const (
	defaultLoggerName = "level"
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
	common := new(commonFlags)

	cmd := &cobra.Command{
		Use:   "log [<type>/]<name>[.<namespace>]",
		Short: "Retrieves logging levels of the Ztunnel instance in the specified pod.",
		Long:  "Retrieve information about logging levels of the Ztunnel instance in the specified pod, and update optionally.",
		Example: `  # Retrieve information about logging levels from all Ztunnel pods
 istioctl ztunnel-config log

 # Update levels of the all loggers for a specific Ztunnel pod
 istioctl ztunnel-config log <pod-name[.namespace]> --level off

 # Update levels of the specified loggers for all Ztunnl pods
 istioctl ztunnel-config log --level access:debug,info

 # Reset levels of all the loggers to default value (warning)  for a specific Ztunnel pod.
 istioctl ztunnel-config log <pod-name[.namespace]> -r
`,
		Aliases: []string{"o"},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := common.validateArgs(cmd, args); err != nil {
				return err
			}
			if reset && loggerLevelString != "" {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("--level cannot be combined with --reset")
			}
			if common.outputFormat != "" && common.outputFormat != summaryOutput {
				return fmt.Errorf("--output is not applicable for this command")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			var podNames []string
			var podNamespace string
			if len(args) == 1 {
				podName, ns, err := getComponentPodName(ctx, args[0], false)
				if err != nil {
					return err
				}
				ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, ns)
				if !ztunnelPod {
					return fmt.Errorf("workloads command is only supported by Ztunnel proxies: %v", podName)
				}
				podNames = []string{podName}
				podNamespace = ns
			} else {
				var err error
				podNames, podNamespace, err = ctx.InferPodsFromTypedResource("daemonset/ztunnel", ctx.IstioNamespace())
				if err != nil {
					return err
				}
			}

			destLoggerLevels := map[string]Level{}
			if reset {
				log.Warn("log level reset; using default value \"info\" for Ztunnel")
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
				resp, err := setupZtunnelLogs(kubeClient, q, podName, podNamespace, common.proxyAdminPort)
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
	common.attach(cmd)
	cmd.PersistentFlags().BoolVarP(&reset, "reset", "r", reset, "Reset levels to default value (warning).")
	cmd.PersistentFlags().StringVar(&loggerLevelString, "level", loggerLevelString,
		fmt.Sprintf("Comma-separated minimum per-logger level of messages to output, in the form of"+
			" [<logger>:]<level>,[<logger>:]<level>,... or <level> to change all active loggers, "+
			"where logger components can be listed by running \"istioctl ztunnel-config log <pod-name[.namespace]>\""+
			", and level can be one of %s", levelListString))
	return cmd
}

func setupZtunnelLogs(kubeClient kube.CLIClient, param, podName, podNamespace string, port int) (string, error) {
	path := "logging"
	if param != "" {
		path = path + "?" + param
	}
	// "Envoy" applies despite this being ztunnel
	result, err := kubeClient.EnvoyDoWithPort(context.TODO(), podName, podNamespace, "POST", path, port)
	if err != nil {
		return "", fmt.Errorf("failed to execute command on Ztunnel: %v", err)
	}
	return string(result), nil
}

// getComponentPodName returns the pod name and namespace of the Istio component
func getComponentPodName(ctx cli.Context, podflag string, enforceSinglePod bool) (string, string, error) {
	// If user passed --namespace, respect it. Else fallback to --istio-namespace (which is typically defaulted, to istio-system).
	return getPodNameWithNamespace(ctx, podflag, enforceSinglePod, model.GetOrDefault(ctx.Namespace(), ctx.IstioNamespace()))
}

func getPodNameWithNamespace(ctx cli.Context, podflag string, enforceSinglePod bool, ns string) (string, string, error) {
	if enforceSinglePod {
		pods, podNamespace, err := ctx.InferPodsFromTypedResource(podflag, ns)
		if err != nil {
			return "", "", err
		}
		if len(pods) != 1 {
			return "", "", fmt.Errorf("ztunnel pod name or --node must be set")
		}
		return pods[0], podNamespace, nil
	}
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
	cw := &ztunnelDump.ConfigWriter{Stdout: out, FullDump: debug}
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

// runConfigDump runs a function that acts on a configdump.
// enforceSinglePod can be set for commands where we enforce a user to explicitly specify a pod. This should be 'true' when
// the content is specific to a single Ztunnel pod, but 'false' if it is the same across all pods.
func runConfigDump(
	ctx cli.Context,
	common *commonFlags,
	enforceSinglePod bool,
	f func(cw *ztunnelDump.ConfigWriter) error,
) func(c *cobra.Command, args []string) error {
	return func(c *cobra.Command, args []string) error {
		var podName, podNamespace string
		kubeClient, err := ctx.CLIClient()
		if err != nil {
			return err
		}
		var configWriter *ztunnelDump.ConfigWriter
		if common.configDumpFile != "" {
			configWriter, err = setupFileZtunnelConfigdumpWriter(common.configDumpFile, c.OutOrStdout())
		} else {
			lookup := "daemonset/ztunnel"
			if len(args) > 0 {
				lookup = args[0]
				// If they explicitly asked for an unreliable container, allow it
				enforceSinglePod = false
			}
			if common.node != "" {
				nsn, err := PodOnNodeFromDaemonset(common.node, "ztunnel", ctx.IstioNamespace(), kubeClient)
				if err != nil {
					return err
				}
				podName, podNamespace = nsn.Name, nsn.Namespace
			} else {
				if podName, podNamespace, err = getComponentPodName(ctx, lookup, enforceSinglePod); err != nil {
					return err
				}
			}
			ztunnelPod := ambientutil.IsZtunnelPod(kubeClient, podName, podNamespace)
			if !ztunnelPod {
				return fmt.Errorf("workloads command is only supported by Ztunnel proxies: %v", podName)
			}
			configWriter, err = setupZtunnelConfigDumpWriter(kubeClient, podName, podNamespace, c.OutOrStdout())
		}
		if err != nil {
			return err
		}
		return f(configWriter)
	}
}

func PodOnNodeFromDaemonset(node string, name, namespace string, client kube.Client) (types.NamespacedName, error) {
	ds, err := client.Kube().AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return types.NamespacedName{}, err
	}
	selector := ds.Spec.Selector
	if selector == nil {
		return types.NamespacedName{}, fmt.Errorf("selector is required")
	}

	sel := selector.MatchLabels
	kv := []string{}
	for k, v := range sel {
		kv = append(kv, k+"="+v)
	}
	podsr, err := client.Kube().CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		TypeMeta:      metav1.TypeMeta{},
		LabelSelector: strings.Join(kv, ","),
		FieldSelector: "spec.nodeName=" + node,
	})
	if err != nil {
		return types.NamespacedName{}, err
	}
	pods := slices.Reference(podsr.Items)
	if len(pods) > 0 {
		// We need to pass in a sorter, and the one used by `kubectl logs` is good enough.
		sortBy := func(pods []*corev1.Pod) sort.Interface { return podutils.ByLogging(pods) }
		sort.Sort(sortBy(pods))
		return config.NamespacedName(pods[0]), nil
	}
	return types.NamespacedName{}, fmt.Errorf("no pods found")
}

type commonFlags struct {
	// output format (json, yaml or short)
	outputFormat string

	proxyAdminPort int

	configDumpFile string

	node string
}

func (c *commonFlags) attach(cmd *cobra.Command) {
	cmd.PersistentFlags().IntVar(&c.proxyAdminPort, "proxy-admin-port", kube.DefaultProxyAdminPort, "Ztunnel proxy admin port")
	cmd.PersistentFlags().StringVarP(&c.outputFormat, "output", "o", summaryOutput, "Output format: one of json|yaml|short")
	cmd.PersistentFlags().StringVar(&c.node, "node", "", "Filter workloads by node field")
	cmd.PersistentFlags().StringVarP(&c.configDumpFile, "file", "f", "",
		"Ztunnel config dump JSON file")
}

func (c *commonFlags) validateArgs(cmd *cobra.Command, args []string) error {
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
	if set > 1 {
		cmd.Println(cmd.UsageString())
		return fmt.Errorf("at most one of --file, --node, or pod name must be passed")
	}
	return nil
}
