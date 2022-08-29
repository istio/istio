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
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/install"
	udsLog "istio.io/istio/cni/pkg/log"
	"istio.io/istio/cni/pkg/monitoring"
	"istio.io/istio/cni/pkg/repair"
	"istio.io/istio/pkg/cmd"
	iptables "istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/collateral"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	logOptions   = log.DefaultOptions()
	ctrlzOptions = ctrlz.DefaultOptions()
)

var rootCmd = &cobra.Command{
	Use:          "install-cni",
	Short:        "Install and configure Istio CNI plugin on a node, detect and repair pod which is broken by race condition.",
	SilenceUsage: true,
	PreRunE: func(c *cobra.Command, args []string) error {
		if err := log.Configure(logOptions); err != nil {
			log.Errorf("Failed to configure log %v", err)
		}
		return nil
	},
	RunE: func(c *cobra.Command, args []string) (err error) {
		cmd.PrintFlags(c.Flags())
		ctx := c.Context()

		// Start controlz server
		_, _ = ctrlz.Run(ctrlzOptions, nil)

		var cfg *config.Config
		if cfg, err = constructConfig(); err != nil {
			return
		}
		log.Infof("CNI install configuration: \n%+v", cfg.InstallConfig)
		log.Infof("CNI race repair configuration: \n%+v", cfg.RepairConfig)

		// Start metrics server
		monitoring.SetupMonitoring(cfg.InstallConfig.MonitoringPort, "/metrics", ctx.Done())

		// Start UDS log server
		udsLogger := udsLog.NewUDSLogger()
		if err = udsLogger.StartUDSLogServer(cfg.InstallConfig.LogUDSAddress, ctx.Done()); err != nil {
			log.Errorf("Failed to start up UDS Log Server: %v", err)
			return
		}

		isReady := install.StartServer()

		installer := install.NewInstaller(&cfg.InstallConfig, isReady)

		repair.StartRepair(ctx, &cfg.RepairConfig)

		if err = installer.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("Installer exits with %v", err)
				// Error was caused by interrupt/termination signal
				err = nil
			} else {
				log.Errorf("Installer exits with %v", err)
			}
		}

		if cleanErr := installer.Cleanup(); cleanErr != nil {
			if err != nil {
				err = fmt.Errorf("%s: %w", cleanErr.Error(), err)
			} else {
				err = cleanErr
			}
		}

		return
	},
}

// GetCommand returns the main cobra.Command object for this application
func GetCommand() *cobra.Command {
	return rootCmd
}

func init() {
	viper.AutomaticEnv()
	viper.AllowEmptyEnv(true)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	logOptions.AttachCobraFlags(rootCmd)
	ctrlzOptions.AttachCobraFlags(rootCmd)

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio CNI Plugin Installer",
		Section: "install-cni CLI",
		Manual:  "Istio CNI Plugin Installer",
	}))

	registerStringParameter(constants.CNINetDir, "/etc/cni/net.d", "Directory on the host where CNI network plugins are installed")
	registerStringParameter(constants.CNIConfName, "", "Name of the CNI configuration file")
	registerBooleanParameter(constants.ChainedCNIPlugin, true, "Whether to install CNI plugin as a chained or standalone")
	registerStringParameter(constants.CNINetworkConfig, "", "CNI configuration template as a string")
	registerBooleanParameter(constants.CNIEnableInstall, true, "Whether to install CNI configuration and binary files")
	registerBooleanParameter(constants.CNIEnableReinstall, true, "Whether to reinstall CNI configuration and binary files")
	registerStringParameter(constants.LogLevel, "warn", "Fallback value for log level in CNI config file, if not specified in helm template")

	// Not configurable in CNI helm charts
	registerStringParameter(constants.MountedCNINetDir, "/host/etc/cni/net.d", "Directory on the container where CNI networks are installed")
	registerStringParameter(constants.CNINetworkConfigFile, "", "CNI config template as a file")
	registerStringParameter(constants.KubeconfigFilename, "ZZZ-istio-cni-kubeconfig",
		"Name of the kubeconfig file which CNI plugin will use when interacting with API server")
	registerIntegerParameter(constants.KubeconfigMode, constants.DefaultKubeconfigMode, "File mode of the kubeconfig file")
	registerStringParameter(constants.KubeCAFile, "", "CA file for kubeconfig. Defaults to the same as install-cni pod")
	registerBooleanParameter(constants.SkipTLSVerify, false, "Whether to use insecure TLS in kubeconfig file")
	registerBooleanParameter(constants.UpdateCNIBinaries, true, "Whether to refresh existing binaries when installing CNI")
	registerStringArrayParameter(constants.SkipCNIBinaries, []string{},
		"Binaries that should not be installed. Currently Istio only installs one binary `istio-cni`")
	registerIntegerParameter(constants.MonitoringPort, 15014, "HTTP port to serve prometheus metrics")
	registerStringParameter(constants.LogUDSAddress, "/var/run/istio-cni/log.sock", "The UDS server address which CNI plugin will copy log ouptut to")

	// Repair
	registerBooleanParameter(constants.RepairEnabled, true, "Whether to enable race condition repair or not")
	registerBooleanParameter(constants.RepairDeletePods, false, "Controller will delete pods when detecting pod broken by race condition")
	registerBooleanParameter(constants.RepairLabelPods, false, "Controller will label pods when detecting pod broken by race condition")
	registerBooleanParameter(constants.RepairRunAsDaemon, false, "Controller will run in a loop")
	registerStringParameter(constants.RepairLabelKey, "cni.istio.io/uninitialized",
		"The key portion of the label which will be set by the ace repair if label pods is true")
	registerStringParameter(constants.RepairLabelValue, "true",
		"The value portion of the label which will be set by the race repair if label pods is true")
	registerStringParameter(constants.RepairNodeName, "", "The name of the managed node (will manage all nodes if unset)")
	registerStringParameter(constants.RepairSidecarAnnotation, "sidecar.istio.io/status",
		"An annotation key that indicates this pod contains an istio sidecar. All pods without this annotation will be ignored."+
			"The value of the annotation is ignored.")
	registerStringParameter(constants.RepairInitContainerName, "istio-validation",
		"The name of the istio init container (will crash-loop if CNI is not configured for the pod)")
	registerStringParameter(constants.RepairInitTerminationMsg, "",
		"The expected termination message for the init container when crash-looping because of CNI misconfiguration")
	registerIntegerParameter(constants.RepairInitExitCode, iptables.ValidationErrorCode,
		"Expected exit code for the init container when crash-looping because of CNI misconfiguration")
	registerStringParameter(constants.RepairLabelSelectors, "",
		"A set of label selectors in label=value format that will be added to the pod list filters")
	registerStringParameter(constants.RepairFieldSelectors, "",
		"A set of field selectors in label=value format that will be added to the pod list filters")
}

func registerStringParameter(name, value, usage string) {
	rootCmd.Flags().String(name, value, usage)
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	// Note: we do not rely on istio env package to retrieve configuration. We relies on viper.
	// This is just to make sure the reference doc tool can generate doc with these vars as env variable at istio.io.
	env.Register(envName, value, usage)
	bindViper(name)
}

func registerStringArrayParameter(name string, value []string, usage string) {
	rootCmd.Flags().StringArray(name, value, usage)
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	// Note: we do not rely on istio env package to retrieve configuration. We relies on viper.
	// This is just to make sure the reference doc tool can generate doc with these vars as env variable at istio.io.
	env.Register(envName, strings.Join(value, ","), usage)
	bindViper(name)
}

func registerIntegerParameter(name string, value int, usage string) {
	rootCmd.Flags().Int(name, value, usage)
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	// Note: we do not rely on istio env package to retrieve configuration. We relies on viper.
	// This is just to make sure the reference doc tool can generate doc with these vars as env variable at istio.io.
	env.Register(envName, value, usage)
	bindViper(name)
}

func registerBooleanParameter(name string, value bool, usage string) {
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	rootCmd.Flags().Bool(name, value, usage)
	// Note: we do not rely on istio env package to retrieve configuration. We relies on viper.
	// This is just to make sure the reference doc tool can generate doc with these vars as env variable at istio.io.
	env.Register(envName, value, usage)
	bindViper(name)
}

func bindViper(name string) {
	if err := viper.BindPFlag(name, rootCmd.Flags().Lookup(name)); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func constructConfig() (*config.Config, error) {
	installCfg := config.InstallConfig{
		CNINetDir:        viper.GetString(constants.CNINetDir),
		MountedCNINetDir: viper.GetString(constants.MountedCNINetDir),
		CNIConfName:      viper.GetString(constants.CNIConfName),
		ChainedCNIPlugin: viper.GetBool(constants.ChainedCNIPlugin),

		CNINetworkConfigFile: viper.GetString(constants.CNINetworkConfigFile),
		CNINetworkConfig:     viper.GetString(constants.CNINetworkConfig),
		CNIEnableInstall:     viper.GetBool(constants.CNIEnableInstall),
		CNIEnableReinstall:   viper.GetBool(constants.CNIEnableReinstall),

		LogLevel:           viper.GetString(constants.LogLevel),
		KubeconfigFilename: viper.GetString(constants.KubeconfigFilename),
		KubeconfigMode:     viper.GetInt(constants.KubeconfigMode),
		KubeCAFile:         viper.GetString(constants.KubeCAFile),
		SkipTLSVerify:      viper.GetBool(constants.SkipTLSVerify),
		K8sServiceProtocol: os.Getenv("KUBERNETES_SERVICE_PROTOCOL"),
		K8sServiceHost:     os.Getenv("KUBERNETES_SERVICE_HOST"),
		K8sServicePort:     os.Getenv("KUBERNETES_SERVICE_PORT"),
		K8sNodeName:        os.Getenv("KUBERNETES_NODE_NAME"),

		CNIBinSourceDir:   constants.CNIBinDir,
		CNIBinTargetDirs:  []string{constants.HostCNIBinDir, constants.SecondaryBinDir},
		UpdateCNIBinaries: viper.GetBool(constants.UpdateCNIBinaries),
		SkipCNIBinaries:   viper.GetStringSlice(constants.SkipCNIBinaries),
		MonitoringPort:    viper.GetInt(constants.MonitoringPort),
		LogUDSAddress:     viper.GetString(constants.LogUDSAddress),
	}

	if len(installCfg.K8sNodeName) == 0 {
		var err error
		installCfg.K8sNodeName, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}

	repairCfg := config.RepairConfig{
		Enabled:            viper.GetBool(constants.RepairEnabled),
		DeletePods:         viper.GetBool(constants.RepairDeletePods),
		LabelPods:          viper.GetBool(constants.RepairLabelPods),
		RunAsDaemon:        viper.GetBool(constants.RepairRunAsDaemon),
		LabelKey:           viper.GetString(constants.RepairLabelKey),
		LabelValue:         viper.GetString(constants.RepairLabelValue),
		NodeName:           viper.GetString(constants.RepairNodeName),
		SidecarAnnotation:  viper.GetString(constants.RepairSidecarAnnotation),
		InitContainerName:  viper.GetString(constants.RepairInitContainerName),
		InitTerminationMsg: viper.GetString(constants.RepairInitTerminationMsg),
		InitExitCode:       viper.GetInt(constants.RepairInitExitCode),
		LabelSelectors:     viper.GetString(constants.RepairLabelSelectors),
		FieldSelectors:     viper.GetString(constants.RepairFieldSelectors),
	}

	return &config.Config{InstallConfig: installCfg, RepairConfig: repairCfg}, nil
}
