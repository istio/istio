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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/viper"

	"istio.io/istio/istioctl/pkg/admin"
	"istio.io/istio/istioctl/pkg/analyze"
	"istio.io/istio/istioctl/pkg/authz"
	"istio.io/istio/istioctl/pkg/checkinject"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/istioctl/pkg/config"
	"istio.io/istio/istioctl/pkg/dashboard"
	"istio.io/istio/istioctl/pkg/describe"
	"istio.io/istio/istioctl/pkg/injector"
	"istio.io/istio/istioctl/pkg/install"
	"istio.io/istio/istioctl/pkg/internaldebug"
	"istio.io/istio/istioctl/pkg/kubeinject"
	"istio.io/istio/istioctl/pkg/metrics"
	"istio.io/istio/istioctl/pkg/multicluster"
	"istio.io/istio/istioctl/pkg/precheck"
	"istio.io/istio/istioctl/pkg/proxyconfig"
	"istio.io/istio/istioctl/pkg/proxystatus"
	"istio.io/istio/istioctl/pkg/revision"
	"istio.io/istio/istioctl/pkg/root"
	"istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/validate"
	"istio.io/istio/istioctl/pkg/version"
	"istio.io/istio/istioctl/pkg/wait"
	"istio.io/istio/istioctl/pkg/waypoint"
	"istio.io/istio/istioctl/pkg/workload"
	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/bug-report/pkg/bugreport"
)

const (
	// Location to read istioctl defaults from
	defaultIstioctlConfig = "$HOME/.istioctl/config.yaml"
)

const (
	FlagCharts = "charts"
)

// ConfigAndEnvProcessing uses spf13/viper for overriding CLI parameters
func ConfigAndEnvProcessing() error {
	configPath := filepath.Dir(root.IstioConfig)
	baseName := filepath.Base(root.IstioConfig)
	configType := filepath.Ext(root.IstioConfig)
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
	if root.IstioConfig != defaultIstioctlConfig {
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
	}

	rootCmd.SetArgs(args)

	flags := rootCmd.PersistentFlags()
	rootOptions := cli.AddRootFlags(flags)

	ctx := cli.NewCLIContext(rootOptions)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := configureLogging(cmd, args); err != nil {
			return err
		}
		ctx.ConfigureDefaultNamespace()
		return nil
	}

	_ = rootCmd.RegisterFlagCompletionFunc(cli.FlagIstioNamespace, func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return completion.ValidNamespaceArgs(cmd, ctx, args, toComplete)
	})
	_ = rootCmd.RegisterFlagCompletionFunc(cli.FlagNamespace, func(
		cmd *cobra.Command, args []string, toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return completion.ValidNamespaceArgs(cmd, ctx, args, toComplete)
	})

	// Attach the Istio logging options to the command.
	root.LoggingOptions.AttachCobraFlags(rootCmd)
	hiddenFlags := []string{
		"log_as_json", "log_rotate", "log_rotate_max_age", "log_rotate_max_backups",
		"log_rotate_max_size", "log_stacktrace_level", "log_target", "log_caller", "log_output_level",
	}
	for _, opt := range hiddenFlags {
		_ = rootCmd.PersistentFlags().MarkHidden(opt)
	}

	cmd.AddFlags(rootCmd)

	kubeInjectCmd := kubeinject.InjectCommand(ctx)
	hideInheritedFlags(kubeInjectCmd, cli.FlagNamespace)
	rootCmd.AddCommand(kubeInjectCmd)

	experimentalCmd := &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"x", "exp"},
		Short:   "Experimental commands that may be modified or deprecated",
	}

	xdsBasedTroubleshooting := []*cobra.Command{
		version.XdsVersionCommand(ctx),
		proxystatus.XdsStatusCommand(ctx),
	}
	debugBasedTroubleshooting := []*cobra.Command{
		version.NewVersionCommand(ctx),
		proxystatus.StatusCommand(ctx),
	}
	var debugCmdAttachmentPoint *cobra.Command
	if viper.GetBool("PREFER-EXPERIMENTAL") {
		legacyCmd := &cobra.Command{
			Use:   "legacy",
			Short: "Legacy command variants",
		}
		rootCmd.AddCommand(legacyCmd)
		for _, c := range xdsBasedTroubleshooting {
			rootCmd.AddCommand(c)
		}
		debugCmdAttachmentPoint = legacyCmd
	} else {
		debugCmdAttachmentPoint = rootCmd
	}
	for _, c := range xdsBasedTroubleshooting {
		experimentalCmd.AddCommand(c)
	}
	for _, c := range debugBasedTroubleshooting {
		debugCmdAttachmentPoint.AddCommand(c)
	}

	rootCmd.AddCommand(experimentalCmd)
	rootCmd.AddCommand(proxyconfig.ProxyConfig(ctx))
	rootCmd.AddCommand(admin.Cmd(ctx))
	experimentalCmd.AddCommand(injector.Cmd(ctx))

	rootCmd.AddCommand(install.NewVerifyCommand())
	rootCmd.AddCommand(mesh.UninstallCmd(root.LoggingOptions))

	experimentalCmd.AddCommand(authz.AuthZ(ctx))
	rootCmd.AddCommand(seeExperimentalCmd("authz"))
	experimentalCmd.AddCommand(metrics.Cmd(ctx))
	experimentalCmd.AddCommand(describe.Cmd(ctx))
	experimentalCmd.AddCommand(wait.Cmd(ctx))
	experimentalCmd.AddCommand(softGraduatedCmd(mesh.UninstallCmd(root.LoggingOptions)))
	experimentalCmd.AddCommand(config.Cmd())
	experimentalCmd.AddCommand(workload.Cmd(ctx))
	experimentalCmd.AddCommand(revision.Cmd(ctx))
	experimentalCmd.AddCommand(internaldebug.DebugCommand(ctx))
	experimentalCmd.AddCommand(precheck.Cmd(ctx))
	experimentalCmd.AddCommand(proxyconfig.StatsConfigCmd(ctx))
	experimentalCmd.AddCommand(checkinject.Cmd(ctx))
	experimentalCmd.AddCommand(waypoint.Cmd(ctx))

	analyzeCmd := analyze.Analyze(ctx)
	hideInheritedFlags(analyzeCmd, cli.FlagIstioNamespace)
	rootCmd.AddCommand(analyzeCmd)

	dashboardCmd := dashboard.Dashboard(ctx)
	hideInheritedFlags(dashboardCmd, cli.FlagNamespace, cli.FlagIstioNamespace)
	rootCmd.AddCommand(dashboardCmd)

	manifestCmd := mesh.ManifestCmd(root.LoggingOptions)
	hideInheritedFlags(manifestCmd, cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(manifestCmd)

	operatorCmd := mesh.OperatorCmd()
	hideInheritedFlags(operatorCmd, cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(operatorCmd)

	installCmd := mesh.InstallCmd(root.LoggingOptions)
	hideInheritedFlags(installCmd, cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(installCmd)

	profileCmd := mesh.ProfileCmd(root.LoggingOptions)
	hideInheritedFlags(profileCmd, cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(profileCmd)

	upgradeCmd := mesh.UpgradeCmd(root.LoggingOptions)
	hideInheritedFlags(upgradeCmd, cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(upgradeCmd)

	bugReportCmd := bugreport.Cmd(ctx, root.LoggingOptions)
	hideInheritedFlags(bugReportCmd, cli.FlagNamespace, cli.FlagIstioNamespace)
	rootCmd.AddCommand(bugReportCmd)

	tagCmd := tag.TagCommand(ctx)
	hideInheritedFlags(tag.TagCommand(ctx), cli.FlagNamespace, cli.FlagIstioNamespace, FlagCharts)
	rootCmd.AddCommand(tagCmd)

	remoteSecretCmd := multicluster.NewCreateRemoteSecretCommand()
	remoteClustersCmd := proxyconfig.ClustersCommand(ctx)
	// leave the multicluster commands in x for backwards compat
	rootCmd.AddCommand(remoteSecretCmd)
	rootCmd.AddCommand(remoteClustersCmd)
	experimentalCmd.AddCommand(remoteSecretCmd)
	experimentalCmd.AddCommand(remoteClustersCmd)

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Control",
		Section: "istioctl CLI",
		Manual:  "Istio Control",
	}))

	validateCmd := validate.NewValidateCommand(ctx)
	hideInheritedFlags(validateCmd, "kubeconfig")
	rootCmd.AddCommand(validateCmd)

	rootCmd.AddCommand(optionsCommand(rootCmd))

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
			return util.CommandParseError{Err: e}
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
	if err := log.Configure(root.LoggingOptions); err != nil {
		return err
	}
	return nil
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
