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
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/spf13/cobra"

	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server"
	istiocmd "istio.io/istio/pkg/cmd"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

var (
	resyncPeriod   time.Duration
	kubeConfig     string
	loggingOptions = log.DefaultOptions()
)

// GetRootCmd returns the root of the cobra command-tree.
func serverCmd() *cobra.Command {

	var (
		serverArgs               = server.DefaultArgs()
		validationArgs           = validation.DefaultArgs()
		livenessProbeOptions     probe.Options
		readinessProbeOptions    probe.Options
		livenessProbeController  probe.Controller
		readinessProbeController probe.Controller
		monitoringPort           uint
		enableProfiling          bool
		pprofPort                uint
	)

	serverCmd := &cobra.Command{
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

			serverArgs.KubeConfig = kubeConfig
			serverArgs.ResyncPeriod = resyncPeriod

			if livenessProbeOptions.IsValid() {
				livenessProbeController = probe.NewFileController(&livenessProbeOptions)
			}
			if readinessProbeOptions.IsValid() {
				readinessProbeController = probe.NewFileController(&readinessProbeOptions)
			}

			// validation tls args fall back to server arg values
			// since the default value for these flags is an empty string, zero length indicates not set
			if len(validationArgs.CACertFile) < 1 {
				validationArgs.CACertFile = serverArgs.CredentialOptions.CACertificateFile
			}
			if len(validationArgs.CertFile) < 1 {
				validationArgs.CertFile = serverArgs.CredentialOptions.CertificateFile
			}
			if len(validationArgs.KeyFile) < 1 {
				validationArgs.KeyFile = serverArgs.CredentialOptions.KeyFile
			}

			if !serverArgs.EnableServer && !validationArgs.EnableValidation {
				log.Fatala("Galley must be running under at least one mode: server or validation")
			}

			if err := validationArgs.Validate(); err != nil {
				log.Fatalf("Invalid validationArgs: %v", err)
			}

			if serverArgs.EnableServer {
				go server.RunServer(serverArgs, livenessProbeController, readinessProbeController)
			}
			if validationArgs.EnableValidation {
				go validation.RunValidation(validationArgs, kubeConfig, livenessProbeController, readinessProbeController)
			}
			galleyStop := make(chan struct{})
			go server.StartSelfMonitoring(galleyStop, monitoringPort)

			if enableProfiling {
				go server.StartProfiling(galleyStop, pprofPort)
			}

			go server.StartProbeCheck(livenessProbeController, readinessProbeController, galleyStop)
			istiocmd.WaitSignal(galleyStop)
		},
	}

	serverCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	serverCmd.PersistentFlags().StringVar(&kubeConfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	serverCmd.PersistentFlags().DurationVar(&resyncPeriod, "resyncPeriod", 0,
		"Resync period for rescanning Kubernetes resources")
	serverCmd.PersistentFlags().StringVar(&serverArgs.CredentialOptions.CertificateFile, "tlsCertFile", "/etc/certs/cert-chain.pem",
		"File containing the x509 Certificate for HTTPS.")
	serverCmd.PersistentFlags().StringVar(&serverArgs.CredentialOptions.KeyFile, "tlsKeyFile", "/etc/certs/key.pem",
		"File containing the x509 private key matching --tlsCertFile.")
	serverCmd.PersistentFlags().StringVar(&serverArgs.CredentialOptions.CACertificateFile, "caCertFile", "/etc/certs/root-cert.pem",
		"File containing the caBundle that signed the cert/key specified by --tlsCertFile and --tlsKeyFile.")
	serverCmd.PersistentFlags().StringVar(&livenessProbeOptions.Path, "livenessProbePath", server.DefaultLivenessProbeFilePath,
		"Path to the file for the Galley liveness probe.")
	serverCmd.PersistentFlags().DurationVar(&livenessProbeOptions.UpdateInterval, "livenessProbeInterval", server.DefaultProbeCheckInterval,
		"Interval of updating file for the Galley liveness probe.")
	serverCmd.PersistentFlags().StringVar(&readinessProbeOptions.Path, "readinessProbePath", server.DefaultReadinessProbeFilePath,
		"Path to the file for the Galley readiness probe.")
	serverCmd.PersistentFlags().DurationVar(&readinessProbeOptions.UpdateInterval, "readinessProbeInterval", server.DefaultProbeCheckInterval,
		"Interval of updating file for the Galley readiness probe.")
	serverCmd.PersistentFlags().UintVar(&monitoringPort, "monitoringPort", 15014,
		"Port to use for exposing self-monitoring information")
	serverCmd.PersistentFlags().UintVar(&pprofPort, "pprofPort", 9094, "Port to use for exposing profiling")
	serverCmd.PersistentFlags().BoolVar(&enableProfiling, "enableProfiling", false,
		"Enable profiling for Galley")

	// server config
	serverCmd.PersistentFlags().StringVarP(&serverArgs.APIAddress, "server-address", "", serverArgs.APIAddress,
		"Address to use for Galley's gRPC API, e.g. tcp://localhost:9092 or unix:///path/to/file")
	serverCmd.PersistentFlags().UintVarP(&serverArgs.MaxReceivedMessageSize, "server-maxReceivedMessageSize", "", serverArgs.MaxReceivedMessageSize,
		"Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&serverArgs.MaxConcurrentStreams, "server-maxConcurrentStreams", "", serverArgs.MaxConcurrentStreams,
		"Maximum number of outstanding RPCs per connection")
	serverCmd.PersistentFlags().BoolVarP(&serverArgs.Insecure, "insecure", "", serverArgs.Insecure,
		"Use insecure gRPC communication")
	serverCmd.PersistentFlags().BoolVar(&serverArgs.EnableServer, "enable-server", serverArgs.EnableServer, "Run galley server mode")
	serverCmd.PersistentFlags().StringVarP(&serverArgs.AccessListFile, "accessListFile", "", serverArgs.AccessListFile,
		"The access list yaml file that contains the allowd mTLS peer ids.")
	serverCmd.PersistentFlags().StringVar(&serverArgs.ConfigPath, "configPath", serverArgs.ConfigPath,
		"Istio config file path")
	serverCmd.PersistentFlags().StringVar(&serverArgs.MeshConfigFile, "meshConfigFile", serverArgs.MeshConfigFile,
		"Path to the mesh config file")
	serverCmd.PersistentFlags().StringVar(&serverArgs.DomainSuffix, "domain", serverArgs.DomainSuffix,
		"DNS domain suffix")
	serverCmd.PersistentFlags().BoolVar(&serverArgs.DisableResourceReadyCheck, "disableResourceReadyCheck", serverArgs.DisableResourceReadyCheck,
		"Disable resource readiness checks. This allows Galley to start if not all resource types are supported")
	serverCmd.PersistentFlags().StringSliceVar(&serverArgs.ExcludedResourceKinds, "excludedResourceKinds",
		serverArgs.ExcludedResourceKinds, "Comma-separated list of resource kinds that should not generate source events")
	serverCmd.PersistentFlags().StringVar(&serverArgs.SinkAddress, "sinkAddress",
		serverArgs.SinkAddress, "Address of MCP Resource Sink server for Galley to connect to. Ex: 'foo.com:1234'")
	serverCmd.PersistentFlags().StringVar(&serverArgs.SinkAuthMode, "sinkAuthMode",
		serverArgs.SinkAuthMode, "Name of authentication plugin to use for connection to sink server.")
	serverCmd.PersistentFlags().StringSliceVar(&serverArgs.SinkMeta, "sinkMeta",
		serverArgs.SinkMeta, "Comma-separated list of key=values to attach as metadata to outgoing sink connections. Ex: 'key=value,key2=value2'")
	serverCmd.PersistentFlags().BoolVar(&serverArgs.EnableServiceDiscovery, "enableServiceDiscovery", false,
		"Enable service discovery processing in Galley")

	// validation config
	serverCmd.PersistentFlags().StringVar(&validationArgs.WebhookConfigFile,
		"validation-webhook-config-file", "",
		"File that contains k8s validatingwebhookconfiguration yaml. Validation is disabled if file is not specified")
	serverCmd.PersistentFlags().UintVar(&validationArgs.Port, "validation-port", 443,
		"HTTPS port of the validation service. Must be 443 if service has more than one port ")
	serverCmd.PersistentFlags().BoolVar(&validationArgs.EnableValidation, "enable-validation", validationArgs.EnableValidation,
		"Run galley validation mode")
	serverCmd.PersistentFlags().StringVar(&validationArgs.DeploymentAndServiceNamespace, "deployment-namespace", "istio-system",
		"Namespace of the deployment for the validation pod")
	serverCmd.PersistentFlags().StringVar(&validationArgs.DeploymentName, "deployment-name", "istio-galley",
		"Name of the deployment for the validation pod")
	serverCmd.PersistentFlags().StringVar(&validationArgs.ServiceName, "service-name", "istio-galley",
		"Name of the validation service running in the same namespace as the deployment")
	serverCmd.PersistentFlags().StringVar(&validationArgs.WebhookName, "webhook-name", "istio-galley",
		"Name of the k8s validatingwebhookconfiguration")

	// Hidden, file only flags for validation specific TLS
	serverCmd.PersistentFlags().StringVar(&validationArgs.CertFile, "validation.tls.clientCertificate", "",
		"File containing the x509 Certificate for HTTPS validation.")
	_ = serverCmd.PersistentFlags().MarkHidden("validation.tls.clientCertificate")
	serverCmd.PersistentFlags().StringVar(&validationArgs.KeyFile, "validation.tls.privateKey", "",
		"File containing the x509 private key matching --validation.tls.clientCertificate.")
	_ = serverCmd.PersistentFlags().MarkHidden("validation.tls.privateKey")
	serverCmd.PersistentFlags().StringVar(&validationArgs.CACertFile, "validation.tls.caCertificates", "",
		"File containing the caBundle that signed the cert/key specified by --validation.tls.clientCertificate and --validation.tls.privateKey.")
	_ = serverCmd.PersistentFlags().MarkHidden("validation.tls.caCertificates")

	serverArgs.IntrospectionOptions.AttachCobraFlags(serverCmd)
	loggingOptions.AttachCobraFlags(serverCmd)
	_ = viper.BindPFlags(serverCmd.PersistentFlags())

	cobra.OnInitialize(setupAliases)

	return serverCmd
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
	viper.RegisterAlias("processing.domainSuffix", "domain")
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
