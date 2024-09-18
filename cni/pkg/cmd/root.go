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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sigs.k8s.io/yaml"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/install"
	udsLog "istio.io/istio/cni/pkg/log"
	"istio.io/istio/cni/pkg/monitoring"
	"istio.io/istio/cni/pkg/nodeagent"
	"istio.io/istio/cni/pkg/repair"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/env"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	iptables "istio.io/istio/tools/istio-iptables/pkg/constants"
)

var (
	logOptions   = istiolog.DefaultOptions()
	log          = scopes.CNIAgent
	ctrlzOptions = func() *ctrlz.Options {
		o := ctrlz.DefaultOptions()
		o.EnablePprof = true
		return o
	}()
)

var rootCmd = &cobra.Command{
	Use:          "install-cni",
	Short:        "Install and configure Istio CNI plugin on a node, detect and repair pod which is broken by race condition.",
	SilenceUsage: true,
	PreRunE: func(c *cobra.Command, args []string) error {
		if err := istiolog.Configure(logOptions); err != nil {
			log.Errorf("Failed to configure log %v", err)
		}
		return nil
	},
	RunE: func(c *cobra.Command, args []string) (err error) {
		ctx := c.Context()

		// Start controlz server
		_, _ = ctrlz.Run(ctrlzOptions, nil)

		var cfg *config.Config
		if cfg, err = constructConfig(); err != nil {
			return
		}
		log.Infof("CNI version: %v", version.Info.String())
		log.Infof("CNI logging level: %+v", istiolog.LevelToString(log.GetOutputLevel()))
		log.Infof("CNI install configuration: \n%+v", cfg.InstallConfig)
		log.Infof("CNI race repair configuration: \n%+v", cfg.RepairConfig)

		// Start metrics server
		monitoring.SetupMonitoring(cfg.InstallConfig.MonitoringPort, "/metrics", ctx.Done())

		// Start UDS log server
		udsLogger := udsLog.NewUDSLogger(log.GetOutputLevel())
		if err = udsLogger.StartUDSLogServer(filepath.Join(cfg.InstallConfig.CNIAgentRunDir, constants.LogUDSSocketName), ctx.Done()); err != nil {
			log.Errorf("Failed to start up UDS Log Server: %v", err)
			return
		}

		// Creates a basic health endpoint server that reports health status
		// based on atomic flag, as set by installer
		// TODO nodeagent watch server should affect this too, and drop atomic flag
		installDaemonReady, watchServerReady := nodeagent.StartHealthServer()

		installer := install.NewInstaller(&cfg.InstallConfig, installDaemonReady)

		if cfg.InstallConfig.AmbientEnabled {
			// Start ambient controller

			// node agent will spawn a goroutine and watch the K8S API for events,
			// as well as listen for messages from the CNI binary.
			cniEventAddr := filepath.Join(cfg.InstallConfig.CNIAgentRunDir, constants.CNIEventSocketName)
			log.Infof("Starting ambient node agent with inpod redirect mode on socket %s", cniEventAddr)

			// instantiate and validate the ambient enablement selector
			selectors := []util.EnablementSelector{}
			if err = yaml.Unmarshal([]byte(cfg.InstallConfig.AmbientEnablementSelector), &selectors); err != nil {
				return fmt.Errorf("failed to parse ambient enablement selector: %v", err)
			}
			compiledSelectors, err := util.NewCompiledEnablementSelectors(selectors)
			if err != nil {
				return fmt.Errorf("failed to instantiate ambient enablement selector: %v", err)
			}
			ambientAgent, err := nodeagent.NewServer(ctx, watchServerReady, cniEventAddr,
				nodeagent.AmbientArgs{
					SystemNamespace:            nodeagent.SystemNamespace,
					Revision:                   nodeagent.Revision,
					ServerSocket:               cfg.InstallConfig.ZtunnelUDSAddress,
					EnablementSelector:         compiledSelectors,
					DNSCapture:                 cfg.InstallConfig.AmbientDNSCapture,
					EnableIPv6:                 cfg.InstallConfig.AmbientIPv6,
					ReconcilePodRulesOnStartup: cfg.InstallConfig.AmbientReconcilePodRulesOnStartup,
				})
			if err != nil {
				return fmt.Errorf("failed to create ambient nodeagent service: %v", err)
			}

			// Ambient watch server IS enabled - on shutdown
			// we need to check and see if this is an upgrade.
			//
			// if it is, we do NOT remove the plugin, and do
			// NOT do ambient watch server cleanup
			defer func() {
				var isUpgrade bool
				if cfg.InstallConfig.AmbientDisableSafeUpgrade {
					log.Info("Ambient node agent safe upgrade explicitly disabled via env")
					isUpgrade = false
				} else {
					isUpgrade = ambientAgent.ShouldStopForUpgrade("istio-cni", nodeagent.PodNamespace)
				}
				log.Infof("Ambient node agent shutting down - is upgrade shutdown? %t", isUpgrade)
				// if we are doing an "upgrade shutdown", then
				// we do NOT want to remove/cleanup the CNI plugin.
				//
				// This is important - we want it to remain in place to "stall"
				// new ambient-enabled pods while our replacement spins up.
				if !isUpgrade {
					if cleanErr := installer.Cleanup(); cleanErr != nil {
						log.Error(cleanErr.Error())
					}
				}
				ambientAgent.Stop(isUpgrade)
			}()

			ambientAgent.Start()

			log.Info("Ambient node agent started, starting installer...")

		} else {
			// Ambient not enabled, so this readiness flag is no-op'd
			watchServerReady.Store(true)

			// Ambient watch server not enabled - on shutdown
			// we just need to remove CNI plugin.
			defer func() {
				log.Infof("CNI node agent shutting down")
				if cleanErr := installer.Cleanup(); cleanErr != nil {
					log.Error(cleanErr.Error())
				}
			}()
		}
		// TODO Note that during an "upgrade shutdown" in ambient mode,
		// repair will (necessarily) be unavailable.
		repair.StartRepair(ctx, cfg.RepairConfig)

		// Note that even though we "install" the CNI plugin here *after* we start the node agent,
		// it will block ambient-enabled pods from starting until `watchServerReady` == true
		// (that is, the node agent is ready to respond to plugin events)
		log.Info("initialization complete, watching node CNI dir")
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

	registerStringParameter(constants.CNIConfName, "", "Name of the CNI configuration file")
	registerBooleanParameter(constants.ChainedCNIPlugin, true, "Whether to install CNI plugin as a chained or standalone")
	registerStringParameter(constants.CNINetworkConfig, "", "CNI configuration template as a string")
	registerStringParameter(constants.LogLevel, "warn", "Fallback value for log level in CNI config file, if not specified in helm template")

	// Not configurable in CNI helm charts
	registerStringParameter(constants.MountedCNINetDir, "/host/etc/cni/net.d", "Directory on the container where CNI networks are installed")
	registerStringParameter(constants.CNIAgentRunDir, "/var/run/istio-cni", "Location of the node agent writable path on the node (used for sockets, etc)")
	registerStringParameter(constants.CNINetworkConfigFile, "", "CNI config template as a file")
	registerIntegerParameter(constants.KubeconfigMode, constants.DefaultKubeconfigMode, "File mode of the kubeconfig file")
	registerStringParameter(constants.KubeCAFile, "", "CA file for kubeconfig. Defaults to the same as install-cni pod")
	registerBooleanParameter(constants.SkipTLSVerify, false, "Whether to use insecure TLS in kubeconfig file")
	registerIntegerParameter(constants.MonitoringPort, 15014, "HTTP port to serve prometheus metrics")
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
		MountedCNINetDir: viper.GetString(constants.MountedCNINetDir),
		CNIConfName:      viper.GetString(constants.CNIConfName),
		ChainedCNIPlugin: viper.GetBool(constants.ChainedCNIPlugin),
		CNIAgentRunDir:   viper.GetString(constants.CNIAgentRunDir),

		// Whatever user has set (with --log_output_level) for 'cni-plugin', pass it down to the plugin. It will use this to determine
		// what level to use for itself.
		// This masks the fact we are doing this weird log-over-UDS to users, and allows them to configure it the same way.
		PluginLogLevel:        istiolog.LevelToString(istiolog.FindScope(constants.CNIPluginLogScope).GetOutputLevel()),
		KubeconfigMode:        viper.GetInt(constants.KubeconfigMode),
		KubeCAFile:            viper.GetString(constants.KubeCAFile),
		SkipTLSVerify:         viper.GetBool(constants.SkipTLSVerify),
		K8sServiceProtocol:    os.Getenv("KUBERNETES_SERVICE_PROTOCOL"),
		K8sServiceHost:        os.Getenv("KUBERNETES_SERVICE_HOST"),
		K8sServicePort:        os.Getenv("KUBERNETES_SERVICE_PORT"),
		K8sNodeName:           os.Getenv("KUBERNETES_NODE_NAME"),
		K8sServiceAccountPath: constants.ServiceAccountPath,

		CNIBinSourceDir:  constants.CNIBinDir,
		CNIBinTargetDirs: []string{constants.HostCNIBinDir},
		MonitoringPort:   viper.GetInt(constants.MonitoringPort),

		ExcludeNamespaces: viper.GetString(constants.ExcludeNamespaces),
		PodNamespace:      viper.GetString(constants.PodNamespace),
		ZtunnelUDSAddress: viper.GetString(constants.ZtunnelUDSAddress),

		AmbientEnabled:                    viper.GetBool(constants.AmbientEnabled),
		AmbientEnablementSelector:         viper.GetString(constants.AmbientEnablementSelector),
		AmbientDNSCapture:                 viper.GetBool(constants.AmbientDNSCapture),
		AmbientIPv6:                       viper.GetBool(constants.AmbientIPv6),
		AmbientDisableSafeUpgrade:         viper.GetBool(constants.AmbientDisableSafeUpgrade),
		AmbientReconcilePodRulesOnStartup: viper.GetBool(constants.AmbientReconcilePodRulesOnStartup),
	}

	if len(installCfg.K8sNodeName) == 0 {
		var err error
		installCfg.K8sNodeName, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}

	if mountPoint := os.Getenv("CONTAINER_SANDBOX_MOUNT_POINT"); mountPoint != "" {
		// HostProcess containers have their file systems mounted at C:\hpc which is the mount point.
		// In order to grab the CNI binary from the container FS, we need to make sure we're
		// use a relative path and not an absolute path (since an absolute path would be looking
		// on the host).
		installCfg.CNIBinSourceDir = filepath.Join(mountPoint, installCfg.CNIBinSourceDir)
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
