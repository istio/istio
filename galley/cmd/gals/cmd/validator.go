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

	"os"
	"os/signal"
	"syscall"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util"
)

// waitSignal awaits for SIGINT or SIGTERM and closes the channel
func waitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	close(stop)
	_ = log.Sync()
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

		timeout := time.After(time.Minute)
		cl := common.client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
		for {
			webhooks := []string{pilotWebhookName, mixerWebhookName}
			if err := util.PatchValidatingWebhookConfig(cl, webhookConfigName, webhooks, caCertPem); err == nil {
				return nil
			}

			log.Errorf("Patching caBundle in ValidatingWebhookConfigurations %v failed -- retrying",
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
		Run: func(cmd *cobra.Command, args []string) {
			mixerValidator, err := validation.CreateMixerValidator(flags.kubeConfig)
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
			waitSignal(stop)
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
