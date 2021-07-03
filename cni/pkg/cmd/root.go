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
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/install"
	"istio.io/istio/cni/pkg/monitoring"
	"istio.io/istio/cni/pkg/repair"
	iptables "istio.io/istio/tools/istio-iptables/pkg/constants"
	"istio.io/pkg/log"
)

var rootCmd = &cobra.Command{
	Use:   "install-cni",
	Short: "Install and configure Istio CNI plugin on a node, detect and repair pod which is broken by race condition",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()

		// TODO(bianpengyuan) add log scope for install & repair.
		var cfg *config.Config
		if cfg, err = constructConfig(); err != nil {
			return
		}
		log.Infof("CNI install configuration: \n%+v", cfg.InstallConfig)
		log.Infof("CNI race repair configuration: \n%+v", cfg.RepairConfig)

		// Start metrics server
		monitoring.SetupMonitoring(cfg.InstallConfig.MonitoringPort, "/metrics", ctx.Done())

		isReady := install.StartServer()

		installer := install.NewInstaller(&cfg.InstallConfig, isReady)

		repair.StartRepair(ctx, &cfg.RepairConfig)

		if err = installer.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// Error was caused by interrupt/termination signal
				err = nil
			}
		}

		if cleanErr := installer.Cleanup(); cleanErr != nil {
			if err != nil {
				err = errors.Wrap(err, cleanErr.Error())
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
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	registerStringParameter(constants.CNINetDir, "/etc/cni/net.d", "Directory on the host where CNI networks are installed")
	registerStringParameter(constants.CNIConfName, "", "Name of the CNI configuration file")
	registerBooleanParameter(constants.ChainedCNIPlugin, true, "Whether to install CNI plugin as a chained or standalone")
	registerStringParameter(constants.CNINetworkConfig, "", "CNI config template as a string")
	registerStringParameter(constants.LogLevel, "warn", "Fallback value for log level in CNI config file, if not specified in helm template")

	// Not configurable in CNI helm charts
	registerStringParameter(constants.MountedCNINetDir, "/host/etc/cni/net.d", "Directory on the container where CNI networks are installed")
	registerStringParameter(constants.CNINetworkConfigFile, "", "CNI config template as a file")
	registerStringParameter(constants.KubeconfigFilename, "ZZZ-istio-cni-kubeconfig", "Name of the kubeconfig file")
	registerIntegerParameter(constants.KubeconfigMode, constants.DefaultKubeconfigMode, "File mode of the kubeconfig file")
	registerStringParameter(constants.KubeCAFile, "", "CA file for kubeconfig. Defaults to the pod one")
	registerBooleanParameter(constants.SkipTLSVerify, false, "Whether to use insecure TLS in kubeconfig file")
	registerBooleanParameter(constants.UpdateCNIBinaries, true, "Update binaries")
	registerStringArrayParameter(constants.SkipCNIBinaries, []string{}, "Binaries that should not be installed")
	registerIntegerParameter(constants.MonitoringPort, 15014, "HTTP port to serve metrics")

	// Repair
	registerBooleanParameter(constants.RepairEnabled, true, "Whether to enable race condition repair or not")
	registerBooleanParameter(constants.RepairDeletePods, false, "Controller will delete pods")
	registerBooleanParameter(constants.RepairLabelPods, false, "Controller will label pods")
	registerBooleanParameter(constants.RepairRunAsDaemon, false, "Controller will run in a loop")
	registerStringParameter(constants.RepairLabelKey, "cni.istio.io/uninitialized",
		"The key portion of the label which will be set by the reconciler if --label-pods is true")
	registerStringParameter(constants.RepairLabelValue, "true",
		"The value portion of the label which will be set by the reconciler if --label-pods is true")
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
	bindViper(name)
}

func registerStringArrayParameter(name string, value []string, usage string) {
	rootCmd.Flags().StringArray(name, value, usage)
	bindViper(name)
}

func registerIntegerParameter(name string, value int, usage string) {
	rootCmd.Flags().Int(name, value, usage)
	bindViper(name)
}

func registerBooleanParameter(name string, value bool, usage string) {
	rootCmd.Flags().Bool(name, value, usage)
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
