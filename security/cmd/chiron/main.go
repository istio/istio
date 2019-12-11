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
	"istio.io/istio/security/pkg/k8s/chiron"
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
		Use:   "Istio Certificate Controller",
		Short: "Istio Certificate Controller",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			if !opts.enableController {
				log.Info("Istio Certificate Controller is not enabled, exit")
				return
			}
			runWebhookController()
		},
	}
)

type cliOptions struct {
	kubeConfigFile string

	// The file path of k8s CA certificate
	k8sCaCertFile string

	// The DNS names of the services for which Chiron manage certs
	dnsNames string
	// The secret names of the services for which Chiron manage certs
	secretNames []string
	// The namespaces of the services for which Chiron manage certs
	serviceNamespaces []string

	// The minimum grace period for cert rotation.
	certMinGracePeriod time.Duration

	logOptions *log.Options
	// Currently, no topic is registered for ctrlz yet
	ctrlzOptions *ctrlz.Options

	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	// If certGracePeriodRatio is 0.2, and cert TTL is 24 hours, then the rotation will happen
	// after 24*(1-0.2) hours since the cert is issued.
	certGracePeriodRatio float32

	// Whether enable the certificate controller
	enableController bool
}

func init() {
	flags := rootCmd.Flags()

	flags.BoolVar(&opts.enableController, "enable-controller", false, "Specifies whether enabling "+
		"Istio Certificate Controller.")

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

	flags.StringSliceVar(&opts.secretNames, "secret-names", []string{"istio-webhook-galley",
		"istio-webhook-sidecar-injector"},
		"The secret names of the services (delimited by comma) for which Chiron manage certs; "+
			"must be corresponding to the  parameter.")
	flags.StringVar(&opts.dnsNames, "dns-names", "istio-galley.istio-system.svc,istio-galley.istio-system;"+
		"istio-sidecar-injector.istio-system.svc,istio-sidecar-injector.istio-system",
		"The DNS names of the services (delimited by semicolon) for which Chiron manage certs; "+
			"must be consistent with the secret-names parameter.")
	flags.StringSliceVar(&opts.serviceNamespaces, "namespaces", []string{"istio-system", "istio-system"},
		"The namespaces of the services (delimited by comma) for which Chiron manage certs; "+
			"must be consistent with the secret-names parameter.")

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Chiron: Istio Certificate Controller",
		Section: "Chiron: Istio Certificate Controller",
		Manual:  "Chiron: Istio Certificate Controller",
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

	stopCh := make(chan struct{})

	dnsNames := strings.Split(opts.dnsNames, ";")

	wc, err := chiron.NewWebhookController(opts.certGracePeriodRatio, opts.certMinGracePeriod,
		k8sClient.CoreV1(), k8sClient.AdmissionregistrationV1beta1(), k8sClient.CertificatesV1beta1(),
		opts.k8sCaCertFile, opts.secretNames, dnsNames, opts.serviceNamespaces)

	if err != nil {
		log.Errorf("failed to create certificate controller: %v", err)
		os.Exit(1)
	}

	// Run the controller to manage the lifecycles of certificates
	wc.Run(stopCh)

	istiocmd.WaitSignal(stopCh)
}
