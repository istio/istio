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
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/lang/checker"
	runtimeConfig "istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/validator"
	"istio.io/istio/mixer/pkg/template"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util"
)

// createMixerValidator creates a mixer backend validator.
// TODO(https://github.com/istio/istio/issues/4887) - refactor mixer
// config validation to remove galley dependency on mixer internal
// packages.
func createMixerValidator(kubeconfig string) (store.BackendValidator, error) {
	info := generatedTmplRepo.SupportedTmplInfo
	templates := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		templates[k] = &t
	}
	adapters := config.AdapterInfoMap(adapter.Inventory(), template.NewRepository(info).SupportsTemplate)

	storeURL := fmt.Sprintf("k8s://%s?retry-timeout=%v", kubeconfig, 2*time.Second)

	s, err := store.NewRegistry(config.StoreInventory()...).NewStore(storeURL)
	if err != nil {
		return nil, err
	}
	rv, err := validator.New(checker.NewTypeChecker(), "", s, adapters, templates)
	if err != nil {
		return nil, err
	}
	return store.NewValidator(rv, runtimeConfig.KindMap(adapters, templates)), nil
}

func validatorCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	var (
		webhookConfigName   string
		pilotWebhookName    string
		mixerWebhookName    string
		port                uint
		domainSuffix        string
		certFile            string
		keyFile             string
		caFile              string
		healthCheckInterval time.Duration
		healthCheckFile     string
	)

	patchCert := func(stop chan struct{}) error {
		caCertPem, err := ioutil.ReadFile(caFile)
		if err != nil {
			return err
		}

		client, err := createInterface(flags.kubeConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to Kubernetes API: %v", err)
		}

		timeout := time.After(time.Minute)
		cl := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
		for {
			webhooks := []string{pilotWebhookName, mixerWebhookName}
			if err = util.PatchValidatingWebhookConfig(cl, webhookConfigName, webhooks, caCertPem); err == nil {
				return nil
			}

			log.Errorf("Patching caBundle in ValidatingWebhookConfigurations %v failed: %v -- retrying",
				webhookConfigName, err)
			select {
			case <-stop:
				// canceled by caller
				return nil
			case <-time.After(time.Second):
				// retry
			case <-timeout:
				return fmt.Errorf("timed out trying to patch ValidatingWebhookConfiguration %q {%q and %q})",
					webhookConfigName, pilotWebhookName, mixerWebhookName)
			}
		}
	}

	validatorCmd := &cobra.Command{
		Use:   "validator",
		Short: "Runs an https server for Istio configuration validation.",
		Long: "Runs an https server for Istio configuration validation. " +
			"Uses k8s validating webhooks to validate Pilot and Mixer configuration.",
		Run: func(_ *cobra.Command, args []string) {
			mixerValidator, err := createMixerValidator(flags.kubeConfig)
			if err != nil {
				fatalf("cannot create mixer backend validator for %q: %v", flags.kubeConfig, err)
			}

			params := validation.WebhookParameters{
				MixerValidator:      mixerValidator,
				PilotDescriptor:     bootstrap.ConfigDescriptor,
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

			go func() {
				if err := patchCert(stop); err != nil {
					log.Errorf("Failed to patch webhook config: %v", err)
				}
			}()

			go wh.Run(stop)
			cmd.WaitSignal(stop)
		},
	}

	validatorCmd.PersistentFlags().StringVar(&webhookConfigName,
		"webhook-name", "istio-galley",
		"Name of the ValidatingWebhookConfiguration resource in Kubernetes")
	validatorCmd.PersistentFlags().StringVar(&pilotWebhookName,
		"pilot-webhook-name", "pilot.validation.istio.io",
		"Name of the pilot webhook entry in the webhook config.")
	validatorCmd.PersistentFlags().StringVar(&mixerWebhookName,
		"mixer-webhook-name", "mixer.validation.istio.io",
		"Name of the mixer webhook entry in the webhook config.")
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

	return validatorCmd
}
