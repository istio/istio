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

package main

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	istiocmd "istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/cmd"
	chiron "istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/pkg/collateral"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// DefaultCertGracePeriodRatio is the default length of certificate rotation grace period,
	// configured as the ratio of the certificate TTL.
	DefaultCertGracePeriodRatio = 0.5

	// DefaultMinCertGracePeriod is the default minimum grace period for workload cert rotation.
	DefaultMinCertGracePeriod = 10 * time.Minute
)

var (
	opts = cliOptions{
		logOptions:   log.DefaultOptions(),
		ctrlzOptions: ctrlz.DefaultOptions(),
	}

	rootCmd = &cobra.Command{
		Use:   "Istio Webhook Controller",
		Short: "Istio Webhook Controller",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			if !opts.enableController {
				log.Info("Istio Webhook Controller is not enabled, exit")
				return
			}
			runWebhookController()
		},
	}
)

type cliOptions struct {
	// The namespace of the webhook certificates
	certificateNamespace string
	kubeConfigFile       string

	// The file path of k8s CA certificate
	k8sCaCertFile string

	// The names of the services of mutating webhooks to manage
	// In prototype, only one is supported.
	mutatingWebhookServiceNames string
	// The names of the services of validating webhooks to manage
	// In prototype, only one is supported.
	validatingWebhookServiceNames string

	// The minimum grace period for cert rotation.
	certMinGracePeriod time.Duration

	logOptions *log.Options
	// Currently, no topic is registered for ctrlz yet
	ctrlzOptions *ctrlz.Options

	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	// If certGracePeriodRatio is 0.2, and cert TTL is 24 hours, then the rotation will happen
	// after 24*(1-0.2) hours since the cert is issued.
	certGracePeriodRatio float32

	// Whether enable the webhook controller
	enableController bool
}

func init() {
	flags := rootCmd.Flags()

	flags.BoolVar(&opts.enableController, "enable-controller", false, "Specifies whether enabling "+
		"Istio Webhook Controller.")
	flags.StringVar(&opts.certificateNamespace, "certificate-namespace", "istio-system",
		"The namespace of the webhook certificates.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	// Specifies the file path to k8s CA certificate.
	// The default value is configured based on https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/:
	// the CA certificate bundle is automatically mounted into pods using the default
	// service account at the path /var/run/secrets/kubernetes.io/serviceaccount/ca.crt.
	flags.StringVar(&opts.k8sCaCertFile, "k8s-ca-cert-file", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		"Specifies the file path to k8s CA certificate.")

	// Certificate issuance configuration.
	flags.Float32Var(&opts.certGracePeriodRatio, "cert-grace-period-ratio",
		DefaultCertGracePeriodRatio, "The certificate rotation grace period, as a ratio of the "+
			"certificate TTL.")
	flags.DurationVar(&opts.certMinGracePeriod, "cert-min-grace-period",
		DefaultMinCertGracePeriod, "The minimum certificate rotation grace period.")

	flags.StringVar(&opts.mutatingWebhookServiceNames, "mutating-webhook-service-names", "istio-sidecar-injector",
		"The names of the services of mutating webhooks, separated by comma. Currently, Chiron will only manage the first one.")
	flags.StringVar(&opts.validatingWebhookServiceNames, "validating-webhook-service-names", "istio-galley",
		"The names of the services of validating webhooks, separated by comma. Currently, Chiron will only manage the first one.")

	// Hide the command line options for the prototype
	_ = flags.MarkHidden("enable-controller")
	_ = flags.MarkHidden("certificate-namespace")
	_ = flags.MarkHidden("kube-config")
	_ = flags.MarkHidden("cert-grace-period-ratio")
	_ = flags.MarkHidden("cert-min-grace-period")
	_ = flags.MarkHidden("webhook-config-files")
	_ = flags.MarkHidden("mutating-webhook-service-names")
	_ = flags.MarkHidden("validating-webhook-service-names")

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Chiron: Istio Webhook Controller",
		Section: "Chiron: Istio Webhook Controller",
		Manual:  "Chiron: Istio Webhook Controller",
	}))
	rootCmd.AddCommand(cmd.NewProbeCmd())

	opts.logOptions.AttachCobraFlags(rootCmd)
	opts.ctrlzOptions.AttachCobraFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}

func runWebhookController() {
	_, _ = ctrlz.Run(opts.ctrlzOptions, nil)

	if err := log.Configure(opts.logOptions); err != nil {
		log.Errorf("failed to configure logging (%v)", err)
		os.Exit(1)
	}

	k8sClient, err := kube.CreateClientset(opts.kubeConfigFile, "")
	if err != nil {
		log.Errorf("could not create k8s clientset: %v", err)
		os.Exit(1)
	}

	mutatingWebhookServiceNames := strings.Split(opts.mutatingWebhookServiceNames, ",")
	validatingWebhookServiceNames := strings.Split(opts.validatingWebhookServiceNames, ",")

	stopCh := make(chan struct{})

	wc, err := chiron.NewWebhookController(opts.certGracePeriodRatio, opts.certMinGracePeriod,
		k8sClient.CoreV1(), k8sClient.AdmissionregistrationV1beta1(), k8sClient.CertificatesV1beta1(),
		opts.k8sCaCertFile, opts.certificateNamespace, mutatingWebhookServiceNames,
		validatingWebhookServiceNames)

	if err != nil {
		log.Errorf("failed to create webhook controller: %v", err)
		os.Exit(1)
	}

	// Run the controller to manage the lifecycles of webhook certificates and webhook configurations
	wc.Run(stopCh)

	istiocmd.WaitSignal(stopCh)
}
