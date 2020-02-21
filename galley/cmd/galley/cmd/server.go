// Copyright 2019 Istio Authors
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
	"flag"
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/galley/pkg/server/settings"
	istiocmd "istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/pkg/log"
)

var (
	loggingOptions = log.DefaultOptions()
)

// GetRootCmd returns the root of the cobra command-tree.
func serverCmd() *cobra.Command {

	var (
		serverArgs = settings.DefaultArgs()
	)

	svr := &cobra.Command{
		Use:          "server",
		Short:        "Starts Galley as a server",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%q is an invalid argument", args[0])
			}
			err := log.Configure(loggingOptions)
			return err
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Retrieve Viper values for each Cobra Val Flag
			viper.SetTypeByDefaultValue(true)
			cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
				if reflect.TypeOf(viper.Get(f.Name)).Kind() == reflect.Slice {
					// Viper cannot convert slices to strings, so this is our workaround.
					_ = f.Value.Set(strings.Join(viper.GetStringSlice(f.Name), ","))
				} else {
					_ = f.Value.Set(viper.GetString(f.Name))
				}
			})

			if !serverArgs.EnableServer && !serverArgs.EnableValidationServer {
				log.Fatala("Galley must be running under at least one mode: server or validation")
			}

			if err := serverArgs.ValidationWebhookServerArgs.Validate(); err != nil {
				log.Fatalf("Invalid validation server args: %v", err)
			}
			if err := serverArgs.ValidationWebhookControllerArgs.Validate(); err != nil {
				log.Fatalf("Invalid validation controller args: %v", err)
			}

			s := server.New(serverArgs)
			if err := s.Start(); err != nil {
				log.Fatalf("Error creating server: %v", err)
			}

			galleyStop := make(chan struct{})
			istiocmd.WaitSignal(galleyStop)
			s.Stop()
		},
	}

	svr.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	svr.PersistentFlags().StringVar(&serverArgs.KubeConfig, "kubeconfig", serverArgs.KubeConfig,
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	svr.PersistentFlags().DurationVar(&serverArgs.ResyncPeriod, "resyncPeriod", serverArgs.ResyncPeriod,
		"Resync period for rescanning Kubernetes resources")
	svr.PersistentFlags().StringVar(&serverArgs.CredentialOptions.CertificateFile, "tlsCertFile", constants.DefaultCertChain,
		"File containing the x509 Certificate for HTTPS.")
	svr.PersistentFlags().StringVar(&serverArgs.CredentialOptions.KeyFile, "tlsKeyFile", constants.DefaultKey,
		"File containing the x509 private key matching --tlsCertFile.")
	svr.PersistentFlags().StringVar(&serverArgs.CredentialOptions.CACertificateFile, "caCertFile", constants.DefaultRootCert,
		"File containing the caBundle that signed the cert/key specified by --tlsCertFile and --tlsKeyFile.")
	svr.PersistentFlags().StringVar(&serverArgs.Liveness.Path, "livenessProbePath", serverArgs.Liveness.Path,
		"Path to the file for the Galley liveness probe.")
	svr.PersistentFlags().DurationVar(&serverArgs.Liveness.UpdateInterval, "livenessProbeInterval", serverArgs.Liveness.UpdateInterval,
		"Interval of updating file for the Galley liveness probe.")
	svr.PersistentFlags().StringVar(&serverArgs.Readiness.Path, "readinessProbePath", serverArgs.Readiness.Path,
		"Path to the file for the Galley readiness probe.")
	svr.PersistentFlags().DurationVar(&serverArgs.Readiness.UpdateInterval, "readinessProbeInterval", serverArgs.Readiness.UpdateInterval,
		"Interval of updating file for the Galley readiness probe.")
	svr.PersistentFlags().UintVar(&serverArgs.MonitoringPort, "monitoringPort", serverArgs.MonitoringPort,
		"Port to use for exposing self-monitoring information")
	svr.PersistentFlags().UintVar(&serverArgs.PprofPort, "pprofPort", serverArgs.PprofPort, "Port to use for exposing profiling")
	svr.PersistentFlags().BoolVar(&serverArgs.EnableProfiling, "enableProfiling", serverArgs.EnableProfiling,
		"Enable profiling for Galley")

	// server config
	svr.PersistentFlags().StringVarP(&serverArgs.APIAddress, "server-address", "", serverArgs.APIAddress,
		"Address to use for Galley's gRPC API, e.g. tcp://localhost:9092 or unix:///path/to/file")
	svr.PersistentFlags().UintVarP(&serverArgs.MaxReceivedMessageSize, "server-maxReceivedMessageSize", "", serverArgs.MaxReceivedMessageSize,
		"Maximum size of individual gRPC messages")
	svr.PersistentFlags().UintVarP(&serverArgs.MaxConcurrentStreams, "server-maxConcurrentStreams", "", serverArgs.MaxConcurrentStreams,
		"Maximum number of outstanding RPCs per connection")
	svr.PersistentFlags().BoolVarP(&serverArgs.Insecure, "insecure", "", serverArgs.Insecure,
		"Use insecure gRPC communication")
	svr.PersistentFlags().BoolVar(&serverArgs.EnableServer, "enable-server", serverArgs.EnableServer, "Run galley server mode")
	svr.PersistentFlags().StringVarP(&serverArgs.AccessListFile, "accessListFile", "", serverArgs.AccessListFile,
		"The access list yaml file that contains the allowed mTLS peer ids.")
	svr.PersistentFlags().StringVar(&serverArgs.ConfigPath, "configPath", serverArgs.ConfigPath,
		"Istio config file path")
	svr.PersistentFlags().StringVar(&serverArgs.MeshConfigFile, "meshConfigFile", serverArgs.MeshConfigFile,
		"Path to the mesh config file")
	svr.PersistentFlags().StringVar(&serverArgs.DomainSuffix, "domain", serverArgs.DomainSuffix,
		"DNS domain suffix")
	svr.PersistentFlags().BoolVar(&serverArgs.DisableResourceReadyCheck, "disableResourceReadyCheck", serverArgs.DisableResourceReadyCheck,
		"Disable resource readiness checks. This allows Galley to start if not all resource types are supported")
	_ = svr.PersistentFlags().MarkDeprecated("disableResourceReadyCheck", "")
	svr.PersistentFlags().StringSliceVar(&serverArgs.ExcludedResourceKinds, "excludedResourceKinds",
		serverArgs.ExcludedResourceKinds, "Comma-separated list of resource kinds that should not generate source events")
	_ = svr.PersistentFlags().MarkDeprecated("excludedResourceKinds", "")
	svr.PersistentFlags().StringVar(&serverArgs.SinkAddress, "sinkAddress",
		serverArgs.SinkAddress, "Address of MCP Resource Sink server for Galley to connect to. Ex: 'foo.com:1234'")
	svr.PersistentFlags().StringVar(&serverArgs.SinkAuthMode, "sinkAuthMode",
		serverArgs.SinkAuthMode, "Name of authentication plugin to use for connection to sink server.")
	svr.PersistentFlags().StringSliceVar(&serverArgs.SinkMeta, "sinkMeta",
		serverArgs.SinkMeta, "Comma-separated list of key=values to attach as metadata to outgoing sink connections. Ex: 'key=value,key2=value2'")
	svr.PersistentFlags().BoolVar(&serverArgs.EnableServiceDiscovery, "enableServiceDiscovery", false,
		"Enable service discovery processing in Galley")
	_ = svr.PersistentFlags().Bool("useOldProcessor", false, "Use the old processing pipeline for config processing")
	_ = svr.PersistentFlags().MarkDeprecated("useOldProcessor",
		"--useOldProcessor is deprecated and has no effect. The new pipeline is the only pipeline")
	svr.PersistentFlags().BoolVar(&serverArgs.WatchConfigFiles, "watchConfigFiles", serverArgs.WatchConfigFiles,
		"Enable the Fsnotify for watching config source files on the disk and implicit signaling on a config change. Explicit signaling will still be enabled")
	svr.PersistentFlags().BoolVar(&serverArgs.EnableConfigAnalysis, "enableAnalysis", serverArgs.EnableConfigAnalysis,
		"Enable config analysis service")

	// validation webhook server config
	_ = svr.PersistentFlags().String("validation-webhook-config-file", "", "Setting this file has no effect")
	_ = svr.PersistentFlags().MarkDeprecated("validation-webhook-config-file", "galley no longer reconciles the entire webhook configuration")
	svr.PersistentFlags().UintVar(&serverArgs.ValidationWebhookServerArgs.Port, "validation-port",
		serverArgs.ValidationWebhookServerArgs.Port, "HTTPS port of the validation service.")
	svr.PersistentFlags().BoolVar(&serverArgs.EnableValidationServer, "enable-validation",
		serverArgs.EnableValidationServer, "Run galley validation mode")

	// validation webhook controller config
	svr.PersistentFlags().BoolVar(&serverArgs.EnableValidationController,
		"enable-reconcileWebhookConfiguration", serverArgs.EnableValidationController,
		"Enable reconciliation for webhook configuration.")
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookControllerArgs.WatchedNamespace, "deployment-namespace", "istio-system",
		"Namespace of the deployment for the validation pod")
	_ = svr.PersistentFlags().String("deployment-name", "istio-galley",
		"Name of the deployment for the validation pod")
	_ = svr.PersistentFlags().MarkDeprecated("deployment-name", "")
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookControllerArgs.ServiceName, "service-name", "istio-galley",
		"Name of the validation service running in the same namespace as the deployment")
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookControllerArgs.WebhookConfigName, "webhook-name", "istio-galley",
		"Name of the k8s validatingwebhookconfiguration")

	// Hidden, file only flags for validation specific TLS
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookServerArgs.CertFile, "validation.tls.clientCertificate",
		serverArgs.ValidationWebhookServerArgs.CertFile,
		"File containing the x509 Certificate for HTTPS validation.")
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookServerArgs.KeyFile, "validation.tls.privateKey",
		serverArgs.ValidationWebhookServerArgs.KeyFile,
		"File containing the x509 private key matching --validation.tls.clientCertificate.")
	svr.PersistentFlags().StringVar(&serverArgs.ValidationWebhookControllerArgs.CAPath, "validation.tls.caCertificates",
		serverArgs.ValidationWebhookControllerArgs.CAPath,
		"File containing the caBundle that signed the cert/key specified by --validation.tls.clientCertificate and --validation.tls.privateKey.")

	serverArgs.IntrospectionOptions.AttachCobraFlags(svr)
	loggingOptions.AttachCobraFlags(svr)
	_ = viper.BindPFlags(svr.PersistentFlags())

	cobra.OnInitialize(setupAliases)

	return svr
}

func setupAliases() {
	// setup viper Aliases for hierarchical config files
	// this must be run after all config sources have been read.
	viper.RegisterAlias("general.kubeconfig", "kubeconfig")
	viper.RegisterAlias("general.introspection.port", "ctrlz_port")
	viper.RegisterAlias("general.introspection.address", "ctrlz_address")
	viper.RegisterAlias("general.liveness.path", "livenessProbePath")
	viper.RegisterAlias("general.liveness.interval", "livenessProbeInterval")
	viper.RegisterAlias("general.readiness.path", "readinessProbePath")
	viper.RegisterAlias("general.readiness.interval", "readinessProbeInterval")
	viper.RegisterAlias("general.meshConfigFile", "meshConfigFile")
	viper.RegisterAlias("general.monitoringPort", "monitoringPort")
	viper.RegisterAlias("general.pprofPort", "pprofPort")
	viper.RegisterAlias("general.enable_profiling", "enableProfiling")
	viper.RegisterAlias("processing.analysis.enable", "enableAnalysis")
	viper.RegisterAlias("processing.discovery.enable", "enableServiceDiscovery")
	viper.RegisterAlias("processing.domainSuffix", "domain")
	viper.RegisterAlias("processing.oldprocessor", "useOldProcessor")
	viper.RegisterAlias("processing.server.enable", "enable-server")
	viper.RegisterAlias("processing.server.address", "server-address")
	viper.RegisterAlias("processing.server.maxReceivedMessageSize", "server-maxReceivedMessageSize")
	viper.RegisterAlias("processing.server.maxConcurrentStreams", "server-maxConcurrentStreams")
	viper.RegisterAlias("processing.server.disableResourceReadyCheck", "disableResourceReadyCheck")
	viper.RegisterAlias("processing.server.auth.mtls.clientCertificate", "tlsCertFile")
	viper.RegisterAlias("processing.server.auth.mtls.privateKey", "tlsKeyFile")
	viper.RegisterAlias("processing.server.auth.mtls.caCertificates", "caCertFile")
	viper.RegisterAlias("processing.server.auth.mtls.accessListFile", "accessListFile")
	viper.RegisterAlias("processing.server.auth.insecure", "insecure")
	viper.RegisterAlias("processing.source.kubernetes.resyncPeriod", "resyncPeriod")
	viper.RegisterAlias("processing.source.filesystem.path", "configPath")
	viper.RegisterAlias("validation.enable", "enable-validation")
	viper.RegisterAlias("validation.webhookConfigFile", "validation-webhook-config-file")
	viper.RegisterAlias("validation.webhookPort", "validation-port")
	viper.RegisterAlias("validation.webhookName", "webhook-name")
	viper.RegisterAlias("validation.deploymentName", "deployment-name")
	viper.RegisterAlias("validation.deploymentNamespace", "deployment-namespace")
	viper.RegisterAlias("validation.serviceName", "service-name")
}
