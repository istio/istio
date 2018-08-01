// Copyright 2018 Istio Authors
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
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	runtimeConfig "istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/template"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

// createMixerValidator creates a mixer backend validator.
// TODO(https://github.com/istio/istio/issues/4887) - refactor mixer
// config validation to remove galley dependency on mixer internal
// packages.
func createMixerValidator() (store.BackendValidator, error) {
	info := generatedTmplRepo.SupportedTmplInfo
	templates := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		templates[k] = &t
	}
	adapters := config.AdapterInfoMap(adapter.Inventory(), template.NewRepository(info).SupportsTemplate)
	return store.NewValidator(nil, runtimeConfig.KindMap(adapters, templates)), nil
}

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

func startSelfMonitoring(stop <-chan struct{}, port uint) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
	if err != nil {
		log.Errorf("Unable to listen on monitoring port %v: %v", port, err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle(metricsPath, promhttp.Handler())
	mux.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, err := out.Write([]byte(version.Info.String())); err != nil {
			log.Errorf("Unable to write version string: %v", err)
		}
	})

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			log.Errorf("Monitoring http server failed: %v", err)
			return
		}
	}()

	<-stop
	err = server.Close()
	log.Debugf("Monitoring server terminated: %v", err)
}

func validatorCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	var (
		port                uint
		domainSuffix        string
		certFile            string
		keyFile             string
		caFile              string
		healthCheckInterval time.Duration
		healthCheckFile     string
		monitoringPort      uint
		webhookConfigFile   string
		deploymentNamespace string
		deploymentName      string
	)

	validatorCmd := &cobra.Command{
		Use:   "validator",
		Short: "Runs an https server for Istio configuration validation.",
		Long: "Runs an https server for Istio configuration validation. " +
			"Uses k8s validating webhooks to validate Pilot and Mixer configuration.",
		Run: func(_ *cobra.Command, args []string) {
			mixerValidator, err := createMixerValidator()
			if err != nil {
				fatalf("cannot create mixer backend validator for %q: %v", flags.kubeConfig, err)
			}

			clientset, err := kube.CreateClientset(flags.kubeConfig, "")
			if err != nil {
				fatalf("could not create k8s clientset: %v", err)
			}

			params := validation.WebhookParameters{
				MixerValidator:      mixerValidator,
				PilotDescriptor:     model.IstioConfigTypes,
				DomainSuffix:        domainSuffix,
				Port:                port,
				CertFile:            certFile,
				KeyFile:             keyFile,
				HealthCheckInterval: healthCheckInterval,
				HealthCheckFile:     healthCheckFile,
				WebhookConfigFile:   webhookConfigFile,
				CACertFile:          caFile,
				Clientset:           clientset,
				DeploymentNamespace: deploymentNamespace,
				DeploymentName:      deploymentName,
			}
			wh, err := validation.NewWebhook(params)
			if err != nil {
				fatalf("cannot create validation webhook service: %v", err)
			}

			// Create the stop channel for all of the servers.
			stop := make(chan struct{})

			go wh.Run(stop)
			go startSelfMonitoring(stop, monitoringPort)
			cmd.WaitSignal(stop)
		},
	}

	validatorCmd.PersistentFlags().StringVar(&webhookConfigFile,
		"webhook-config-file", "",
		"File that contains k8s validatingwebhookconfiguration yaml. Validation is disabled if file is not specified")
	validatorCmd.PersistentFlags().UintVar(&port, "port", 443,
		"HTTPS port of the validation service. Must be 443 if service has more than one port ")
	validatorCmd.PersistentFlags().StringVar(&certFile, "tlsCertFile", "/etc/istio/certs/cert-chain.pem",
		"File containing the x509 Certificate for HTTPS.")
	validatorCmd.PersistentFlags().StringVar(&keyFile, "tlsKeyFile", "/etc/istio/certs/key.pem",
		"File containing the x509 private key matching --tlsCertFile.")
	validatorCmd.PersistentFlags().StringVar(&caFile, "caCertFile", "/etc/istio/certs/root-cert.pem",
		"File containing the caBundle that signed the cert/key specified by --tlsCertFile and --tlsKeyFile.")
	validatorCmd.PersistentFlags().DurationVar(&healthCheckInterval, "healthCheckInterval", 0,
		"Configure how frequently the health check file specified by --healthCheckFile should be updated")
	validatorCmd.PersistentFlags().StringVar(&healthCheckFile, "healthCheckFile", "",
		"File that should be periodically updated if health checking is enabled")
	validatorCmd.PersistentFlags().UintVar(&monitoringPort, "monitoringPort", 9093,
		"Port to use for the exposing self-monitoring information")

	validatorCmd.PersistentFlags().StringVar(&deploymentNamespace, "deployment-namespace", "istio-system",
		"Namespace of the deployment for the validation pod")
	validatorCmd.PersistentFlags().StringVar(&deploymentName, "deployment-name", "istio-galley",
		"Name of the deployment for the validation pod")

	return validatorCmd
}
