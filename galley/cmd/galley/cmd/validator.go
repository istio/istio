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
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"k8s.io/api/admissionregistration/v1beta1"
	admissionClient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"

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
	"istio.io/istio/pkg/util"
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

func reload(caCertFile, webhookConfigFile string) (*v1beta1.ValidatingWebhookConfiguration, error) {
	webhookConfigData, err := ioutil.ReadFile(webhookConfigFile)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode validatingwebhookconfiguration from %v: %v", webhookConfigFile, err)
	}
	caCertPem, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return nil, err
	}
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCertPem
	}
	return &webhookConfig, nil
}

func reconcile(client admissionClient.ValidatingWebhookConfigurationInterface, config *v1beta1.ValidatingWebhookConfiguration) error {
	return util.PatchValidatingWebhookConfig(client, config)
}

// reconcileValidatingWebhookConfiguration reconciles the desired validatingwebhookconfiguration.
func reconcileValidatingWebhookConfiguration(stop <-chan struct{}, caCertFile, webhookConfigFile string) error {
	client, err := kube.CreateClientset(flags.kubeConfig, "")
	if err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, file := range []string{caCertFile, webhookConfigFile} {
		watchDir, _ := filepath.Split(file)
		if err = watcher.Watch(watchDir); err != nil {
			return fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	validateClient := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()

	// initial load
	desiredValidatingWebhookConfig, err := reload(caCertFile, webhookConfigFile)
	if err != nil {
		validation.ReportValidationConfigUpdateError(err)
		return err
	}
	if err := util.PatchValidatingWebhookConfig(validateClient, desiredValidatingWebhookConfig); err != nil {
		validation.ReportValidationConfigUpdateError(err)
		return err
	}

	reconcile := func() {
		if err := util.PatchValidatingWebhookConfig(validateClient, desiredValidatingWebhookConfig); err != nil {
			log.Errorf("Could not reconcile %v validatingwebhookconfiguration: %v",
				desiredValidatingWebhookConfig.Name, err)
			validation.ReportValidationConfigUpdateError(err)
		} else {
			validation.ReportValidationConfigUpdate()
		}
	}

	// reconciliation loop
	go func() {
		tickerC := time.NewTicker(time.Second).C
		for {
			select {
			case <-stop:
				return
			case <-tickerC:
				reconcile()
			case <-watcher.Event:
				loaded, err := reload(caCertFile, webhookConfigFile)
				if err != nil {
					log.Errorf("Could not reload ca-cert-file and validatingwebhookconfiguration: %v", err)
					validation.ReportValidationConfigUpdateError(err)
					break
				}
				desiredValidatingWebhookConfig = loaded
				reconcile()
			}
		}
	}()

	return nil
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

			params := validation.WebhookParameters{
				MixerValidator:      mixerValidator,
				PilotDescriptor:     model.IstioConfigTypes,
				DomainSuffix:        domainSuffix,
				Port:                port,
				CertFile:            certFile,
				KeyFile:             keyFile,
				HealthCheckInterval: healthCheckInterval,
				HealthCheckFile:     healthCheckFile,
			}
			wh, err := validation.NewWebhook(params)
			if err != nil {
				fatalf("cannot create validation webhook service: %v", err)
			}

			// Create the stop channel for all of the servers.
			stop := make(chan struct{})

			if webhookConfigFile != "" {
				log.Infof("server-side configuration validation enabled. Using %v for validatingwebhookconfiguration",
					webhookConfigFile)
				if err := reconcileValidatingWebhookConfiguration(stop, caFile, webhookConfigFile); err != nil {
					log.Errorf("could not start validatingwebhookconfiguration reconcilation: %v", err)
				}
			} else {
				log.Info("server-side configuration validation disabled. Enable with --webhook-config-file")
			}

			go wh.Run(stop)
			go startSelfMonitoring(stop, monitoringPort)
			cmd.WaitSignal(stop)
		},
	}

	validatorCmd.PersistentFlags().StringVar(&webhookConfigFile,
		"webhook-config-file", "/etc/istio/config",
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
		"Configure how frequently the health check file specified by --healhCheckFile should be updated")
	validatorCmd.PersistentFlags().StringVar(&healthCheckFile, "healthCheckFile", "",
		"File that should be periodically updated if health checking is enabled")
	validatorCmd.PersistentFlags().UintVar(&monitoringPort, "monitoringPort", 9093,
		"Port to use for the exposing self-monitoring information")

	return validatorCmd
}
