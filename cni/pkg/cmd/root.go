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
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/install"
	udsLog "istio.io/istio/cni/pkg/log"
	"istio.io/istio/cni/pkg/monitoring"
	"istio.io/istio/cni/pkg/nodeagent"
	"istio.io/istio/cni/pkg/repair"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	iptables "istio.io/istio/tools/istio-iptables/pkg/constants"
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

		// Creates a basic health endpoint server that reports health status
		// based on atomic flag, as set by installer
		// TODO nodeagent watch server should affect this too, and drop atomic flag
		installDaemonReady, watchServerReady := nodeagent.StartHealthServer()

		if cfg.InstallConfig.AmbientEnabled {
			// Start ambient controller

			// node agent will spawn a goroutine and watch the K8S API for events,
			// as well as listen for messages from the CNI binary.
			log.Info("Starting ambient node agent with inpod redirect mode")
			ambientAgent, err := nodeagent.NewServer(ctx, watchServerReady, cfg.InstallConfig.CNIEventAddress,
				nodeagent.AmbientArgs{
					SystemNamespace: nodeagent.PodNamespace,
					Revision:        nodeagent.Revision,
					ServerSocket:    cfg.InstallConfig.ZtunnelUDSAddress,
					DNSCapture:      cfg.InstallConfig.AmbientDNSCapture,
				})
			if err != nil {
				return fmt.Errorf("failed to create ambient nodeagent service: %v", err)
			}

			ambientAgent.Start()
			defer ambientAgent.Stop()

			log.Info("Ambient node agent started, starting installer...")

		} else {
			// Ambient not enabled, so this readiness flag is no-op'd
			watchServerReady.Store(true)
		}

		installer := install.NewInstaller(&cfg.InstallConfig, installDaemonReady)

		repair.StartRepair(ctx, cfg.RepairConfig)

		log.Info("Installer created, watching node CNI dir")
		// installer.Run() will block indefinitely, and attempt to permanently "keep"
		// the CNI binary installed.
		if err = installer.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("installer complete: %v", err)
				// Error was caused by interrupt/termination signal
				err = nil
			} else {
				log.Errorf("installer failed: %v", err)
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
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, collateral.Metadata{
		Title:   "Istio CNI Plugin Installer",
		Section: "install-cni CLI",
		Manual:  "Istio CNI Plugin Installer",
	}))

	registerStringParameter(constants.CNINetDir, "/etc/cni/net.d", "Directory on the host where CNI network plugins are installed")
	registerStringParameter(constants.CNIConfName, "", "Name of the CNI configuration file")
	registerBooleanParameter(constants.ChainedCNIPlugin, true, "Whether to install CNI plugin as a chained or standalone")
	registerStringParameter(constants.CNINetworkConfig, "", "CNI configuration template as a string")
	registerStringParameter(constants.LogLevel, "warn", "Fallback value for log level in CNI config file, if not specified in helm template")

	// Not configurable in CNI helm charts
	registerStringParameter(constants.MountedCNINetDir, "/host/etc/cni/net.d", "Directory on the container where CNI networks are installed")
	registerStringParameter(constants.CNINetworkConfigFile, "", "CNI config template as a file")
	registerStringParameter(constants.KubeconfigFilename, "ZZZ-istio-cni-kubeconfig",
		"Name of the kubeconfig file which CNI plugin will use when interacting with API server")
	registerIntegerParameter(constants.KubeconfigMode, constants.DefaultKubeconfigMode, "File mode of the kubeconfig file")
	registerStringParameter(constants.KubeCAFile, "", "CA file for kubeconfig. Defaults to the same as install-cni pod")
	registerBooleanParameter(constants.SkipTLSVerify, false, "Whether to use insecure TLS in kubeconfig file")
	registerIntegerParameter(constants.MonitoringPort, 15014, "HTTP port to serve prometheus metrics")
	registerStringParameter(constants.LogUDSAddress, "/var/run/istio-cni/log.sock", "The UDS server address which CNI plugin will copy log output to")
	registerStringParameter(constants.CNIEventAddress, "/var/run/istio-cni/pluginevent.sock",
		"The UDS server address which CNI plugin will forward ambient pod creation events to")
	registerStringParameter(constants.ZtunnelUDSAddress, "/var/run/ztunnel/ztunnel.sock", "The UDS server address which ztunnel will connect to")
	registerBooleanParameter(constants.AmbientEnabled, false, "Whether ambient controller is enabled")
	// Repair
	registerBooleanParameter(constants.RepairEnabled, true, "Whether to enable race condition repair or not")
	registerBooleanParameter(constants.RepairDeletePods, false, "Controller will delete pods when detecting pod broken by race condition")
	registerBooleanParameter(constants.RepairLabelPods, false, "Controller will label pods when detecting pod broken by race condition")
	registerStringParameter(constants.RepairLabelKey, "cni.istio.io/uninitialized",
		"The key portion of the label which will be set by the race repair if label pods is true")
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
	registerEnvironment(name, value, usage)
}

func registerIntegerParameter(name string, value int, usage string) {
	rootCmd.Flags().Int(name, value, usage)
	registerEnvironment(name, value, usage)
}

func registerBooleanParameter(name string, value bool, usage string) {
	rootCmd.Flags().Bool(name, value, usage)
	registerEnvironment(name, value, usage)
}

func registerEnvironment[T env.Parseable](name string, defaultValue T, usage string) {
	envName := strings.Replace(strings.ToUpper(name), "-", "_", -1)
	// Note: we do not rely on istio env package to retrieve configuration. We relies on viper.
	// This is just to make sure the reference doc tool can generate doc with these vars as env variable at istio.io.
	env.Register(envName, defaultValue, usage)
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
		CNIBinTargetDirs:  []string{constants.HostCNIBinDir},
		MonitoringPort:    viper.GetInt(constants.MonitoringPort),
		LogUDSAddress:     viper.GetString(constants.LogUDSAddress),
		CNIEventAddress:   viper.GetString(constants.CNIEventAddress),
		ZtunnelUDSAddress: viper.GetString(constants.ZtunnelUDSAddress),

		AmbientEnabled:    viper.GetBool(constants.AmbientEnabled),
		AmbientDNSCapture: viper.GetBool(constants.AmbientDNSCapture),
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
		RepairPods:         viper.GetBool(constants.RepairRepairPods),
		DeletePods:         viper.GetBool(constants.RepairDeletePods),
		LabelPods:          viper.GetBool(constants.RepairLabelPods),
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
