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
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/viper"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/util/templates"

	"istio.io/istio/istioctl/pkg/install"
	"istio.io/istio/istioctl/pkg/multicluster"
	"istio.io/istio/istioctl/pkg/validate"
	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/tools/bug-report/pkg/bugreport"
	"istio.io/pkg/collateral"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

// CommandParseError distinguishes an error parsing istioctl CLI arguments from an error processing
type CommandParseError struct {
	e error
}

func (c CommandParseError) Error() string {
	return c.e.Error()
}

const (
	// Location to read istioctl defaults from
	defaultIstioctlConfig = "$HOME/.istioctl/config.yaml"

	// ExperimentalMsg indicate active development and not for production use warning.
	ExperimentalMsg = `THIS COMMAND IS UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.`
)

const (
	FlagNamespace      = "namespace"
	FlagIstioNamespace = "istioNamespace"
	FlagCharts         = "charts"
)

var (
	// IstioConfig is the name of the istioctl config file (if any)
	IstioConfig = env.Register("ISTIOCONFIG", defaultIstioctlConfig,
		"Default values for istioctl flags").Get()

	kubeconfig       string
	configContext    string
	namespace        string
	istioNamespace   string
	defaultNamespace string

	// Create a kubernetes client (or mockClient) for talking to control plane components
	kubeClientWithRevision = newKubeClientWithRevision

	// Create a kubernetes.ExecClient (or mock) for talking to data plane components
	kubeClient = newKubeClient

	loggingOptions = defaultLogOptions()

	// scope is for dev logging.  Warning: log levels are not set by --log_output_level until command is Run().
	scope = log.RegisterScope("cli", "istioctl", 0)
)

func defaultLogOptions() *log.Options {
	o := log.DefaultOptions()

	// These scopes are, at the default "INFO" level, too chatty for command line use
	o.SetOutputLevel("validation", log.ErrorLevel)
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("analysis", log.WarnLevel)
	o.SetOutputLevel("installer", log.WarnLevel)
	o.SetOutputLevel("translator", log.WarnLevel)
	o.SetOutputLevel("adsc", log.WarnLevel)
	o.SetOutputLevel("default", log.WarnLevel)
	o.SetOutputLevel("klog", log.WarnLevel)
	o.SetOutputLevel("kube", log.ErrorLevel)

	return o
}

// ConfigAndEnvProcessing uses spf13/viper for overriding CLI parameters
func ConfigAndEnvProcessing() error {
	configPath := filepath.Dir(IstioConfig)
	baseName := filepath.Base(IstioConfig)
	configType := filepath.Ext(IstioConfig)
	configName := baseName[0 : len(baseName)-len(configType)]
	if configType != "" {
		configType = configType[1:]
	}

	// Allow users to override some variables through $HOME/.istioctl/config.yaml
	// and environment variables.
	viper.SetEnvPrefix("ISTIOCTL")
	viper.AutomaticEnv()
	viper.AllowEmptyEnv(true) // So we can say ISTIOCTL_CERT_DIR="" to suppress certs
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)
	viper.AddConfigPath(configPath)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	err := viper.ReadInConfig()
	// Ignore errors reading the configuration unless the file is explicitly customized
	if IstioConfig != defaultIstioctlConfig {
		return err
	}

	return nil
}

func init() {
	viper.SetDefault("istioNamespace", constants.IstioSystemNamespace)
	viper.SetDefault("xds-port", 15012)
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               "istioctl",
		Short:             "Istio control interface.",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: `Istio configuration command line utility for service operators to
debug and diagnose their Istio mesh.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
		PersistentPreRunE: configureLogging,
	}

	rootCmd.SetArgs(args)

	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "c", "",
		"Kubernetes configuration file")

	rootCmd.PersistentFlags().StringVar(&configContext, "context", "",
		"The name of the kubeconfig context to use")

	rootCmd.PersistentFlags().StringVarP(&istioNamespace, FlagIstioNamespace, "i", viper.GetString(FlagIstioNamespace),
		"Istio system namespace")

	rootCmd.PersistentFlags().StringVarP(&namespace, FlagNamespace, "n", v1.NamespaceAll,
		"Config namespace")

	_ = rootCmd.RegisterFlagCompletionFunc(FlagIstioNamespace, validNamespaceArgs)
	_ = rootCmd.RegisterFlagCompletionFunc(FlagNamespace, validNamespaceArgs)

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)
	hiddenFlags := []string{
		"log_as_json", "log_rotate", "log_rotate_max_age", "log_rotate_max_backups",
		"log_rotate_max_size", "log_stacktrace_level", "log_target", "log_caller", "log_output_level",
	}

	group := templates.CommandGroup{
		Message:  "Available Commands:",
		Commands: []*cobra.Command{},
	}
	for _, opt := range hiddenFlags {
		_ = rootCmd.PersistentFlags().MarkHidden(opt)
	}

	cmd.AddFlags(rootCmd)

	kubeInjectCmd := injectCommand()
	hideInheritedFlags(kubeInjectCmd, FlagNamespace)
	group.Commands = append(group.Commands, kubeInjectCmd)

	experimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}

	xdsBasedTroubleshooting := []*cobra.Command{
		xdsVersionCommand(),
		xdsStatusCommand(),
	}
	debugBasedTroubleshooting := []*cobra.Command{
		newVersionCommand(),
		statusCommand(),
	}
	if viper.GetBool("PREFER-EXPERIMENTAL") {
		legacyCmd := &cobra.Command{
			Use:   "legacy",
			Short: "Legacy command variants",
		}
		group.Commands = append(group.Commands, legacyCmd)
		group.Commands = append(group.Commands, xdsBasedTroubleshooting...)
	} else {
		group.Commands = append(group.Commands, debugBasedTroubleshooting...)
	}
	for _, c := range xdsBasedTroubleshooting {
		experimentalCmd.AddCommand(c)
	}

	group.Commands = append(group.Commands, experimentalCmd, proxyConfig(), adminCmd())
	experimentalCmd.AddCommand(injectorCommand())

	group.Commands = append(group.Commands, install.NewVerifyCommand(), mesh.UninstallCmd(loggingOptions))
	experimentalCmd.AddCommand(AuthZ())
	group.Commands = append(group.Commands, seeExperimentalCmd("authz"))
	experimentalCmd.AddCommand(uninjectCommand())
	experimentalCmd.AddCommand(metricsCmd())
	experimentalCmd.AddCommand(describe())
	experimentalCmd.AddCommand(addToMeshCmd())
	experimentalCmd.AddCommand(removeFromMeshCmd())
	experimentalCmd.AddCommand(waitCmd())
	experimentalCmd.AddCommand(softGraduatedCmd(mesh.UninstallCmd(loggingOptions)))
	experimentalCmd.AddCommand(configCmd())
	experimentalCmd.AddCommand(workloadCommands())
	experimentalCmd.AddCommand(revisionCommand())
	experimentalCmd.AddCommand(debugCommand())
	experimentalCmd.AddCommand(preCheck())
	experimentalCmd.AddCommand(statsConfigCmd())

	analyzeCmd := Analyze()
	hideInheritedFlags(analyzeCmd, FlagIstioNamespace)
	group.Commands = append(group.Commands, analyzeCmd)

	dashboardCmd := dashboard()
	hideInheritedFlags(dashboardCmd, FlagNamespace, FlagIstioNamespace)
	group.Commands = append(group.Commands, dashboardCmd)

	manifestCmd := mesh.ManifestCmd(loggingOptions)
	hideInheritedFlags(manifestCmd, FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, manifestCmd)

	operatorCmd := mesh.OperatorCmd()
	hideInheritedFlags(operatorCmd, FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, operatorCmd)

	installCmd := mesh.InstallCmd(loggingOptions)
	hideInheritedFlags(installCmd, FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, installCmd)

	profileCmd := mesh.ProfileCmd(loggingOptions)
	hideInheritedFlags(profileCmd, FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, profileCmd)

	upgradeCmd := mesh.UpgradeCmd(loggingOptions)
	hideInheritedFlags(upgradeCmd, FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, upgradeCmd)

	bugReportCmd := bugreport.Cmd(loggingOptions)
	hideInheritedFlags(bugReportCmd, FlagNamespace, FlagIstioNamespace)
	group.Commands = append(group.Commands, bugReportCmd)

	tagCmd := tagCommand()
	hideInheritedFlags(tagCommand(), FlagNamespace, FlagIstioNamespace, FlagCharts)
	group.Commands = append(group.Commands, tagCmd)

	remoteSecretCmd := multicluster.NewCreateRemoteSecretCommand()
	remoteClustersCmd := clustersCommand()
	// leave the multicluster commands in x for backwards compat
	group.Commands = append(group.Commands, remoteSecretCmd, remoteClustersCmd)
	experimentalCmd.AddCommand(remoteSecretCmd)
	experimentalCmd.AddCommand(remoteClustersCmd)

	group.Commands = append(group.Commands, collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Control",
		Section: "istioctl CLI",
		Manual:  "Istio Control",
	}))

	validateCmd := validate.NewValidateCommand(&istioNamespace, &namespace)
	hideInheritedFlags(validateCmd, "kubeconfig")
	group.Commands = append(group.Commands, validateCmd, optionsCommand())

	sort.SliceStable(group.Commands, func(i, j int) bool {
		return group.Commands[i].Use < group.Commands[j].Use
	})

	groups := templates.CommandGroups{group}
	groups.Add(rootCmd)

	filters := []string{"options"}
	templates.ActsAsRootCommand(rootCmd, filters, groups...)

	// BFS applies the flag error function to all subcommands
	seenCommands := make(map[*cobra.Command]bool)
	var commandStack []*cobra.Command

	commandStack = append(commandStack, rootCmd)

	for len(commandStack) > 0 {
		n := len(commandStack) - 1
		curCmd := commandStack[n]
		commandStack = commandStack[:n]
		seenCommands[curCmd] = true
		for _, command := range curCmd.Commands() {
			if !seenCommands[command] {
				commandStack = append(commandStack, command)
			}
		}
		curCmd.SetFlagErrorFunc(func(_ *cobra.Command, e error) error {
			return CommandParseError{e}
		})
	}

	return rootCmd
}

func hideInheritedFlags(orig *cobra.Command, hidden ...string) {
	orig.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		for _, hidden := range hidden {
			_ = cmd.Flags().MarkHidden(hidden) // nolint: errcheck
		}

		orig.SetHelpFunc(nil)
		orig.HelpFunc()(cmd, args)
	})
}

func configureLogging(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	defaultNamespace = getDefaultNamespace(kubeconfig)
	return nil
}

func getDefaultNamespace(kubeconfig string) string {
	configAccess := clientcmd.NewDefaultPathOptions()

	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		return v1.NamespaceDefault
	}

	// If a specific context was specified, use that. Otherwise, just use the current context from the kube config.
	selectedContext := config.CurrentContext
	if configContext != "" {
		selectedContext = configContext
	}

	// Use the namespace associated with the selected context as default, if the context has one
	context, ok := config.Contexts[selectedContext]
	if !ok {
		return v1.NamespaceDefault
	}
	if context.Namespace == "" {
		return v1.NamespaceDefault
	}
	return context.Namespace
}

// softGraduatedCmd is used for commands that have graduated, but we still want the old invocation to work.
func softGraduatedCmd(cmd *cobra.Command) *cobra.Command {
	msg := fmt.Sprintf("(%s has graduated. Use `istioctl %s`)", cmd.Name(), cmd.Name())

	newCmd := *cmd
	newCmd.Short = fmt.Sprintf("%s %s", cmd.Short, msg)
	newCmd.RunE = func(c *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
		return cmd.RunE(c, args)
	}

	return &newCmd
}

// seeExperimentalCmd is used for commands that have been around for a release but not graduated from
// Other alternative
// for graduatedCmd see https://github.com/istio/istio/pull/26408
// for softGraduatedCmd see https://github.com/istio/istio/pull/26563
func seeExperimentalCmd(name string) *cobra.Command {
	msg := fmt.Sprintf("(%s is experimental. Use `istioctl experimental %s`)", name, name)
	return &cobra.Command{
		Use:   name,
		Short: msg,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New(msg)
		},
	}
}
