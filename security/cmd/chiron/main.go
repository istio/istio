package main

import (
	"os"
	"time"

	"istio.io/istio/security/pkg/server/monitoring"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/cmd"
	chiron "istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/pkg/collateral"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
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
	// The name of the MutatingWebhookConfiguration to manage
	mutatingWebhookConfigName string
	// The name of the webhook in a webhook configuration
	mutatingWebhookName string
	// TODO (lei-tang): Add the name of the ValidatingWebhookConfiguration to manage

	certTTL    time.Duration
	maxCertTTL time.Duration
	// The minimum grace period for cert rotation.
	certMinGracePeriod time.Duration

	// Monitoring port number
	monitoringPort int

	logOptions *log.Options
	// Currently, no topic is registered for ctrlz yet
	ctrlzOptions *ctrlz.Options

	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	// If certGracePeriodRatio is 0.2, and cert TTL is 24 hours, then the rotation will happen
	// after 24*(1-0.2) hours since the cert is issued.
	certGracePeriodRatio float32

	// Whether to generate PKCS#8 private keys.
	pkcs8Keys bool

	// Enable profiling in monitoring
	enableProfiling bool

	// Whether enable the webhook controller
	enableController bool
}

func fatalf(template string, args ...interface{}) {
	if len(args) > 0 {
		log.Errorf(template, args...)
	} else {
		log.Errorf(template)
	}
	os.Exit(-1)
}

func init() {
	flags := rootCmd.Flags()

	flags.BoolVar(&opts.enableController, "enable-controller", false, "Specifies whether enabling "+
		"Istio Webhook Controller.")
	flags.StringVar(&opts.certificateNamespace, "certificate-namespace", "istio-system",
		"The namespace of the webhook certificates.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	// Monitoring configuration
	flags.IntVar(&opts.monitoringPort, "monitoring-port", 15021, "The port number for monitoring Chiron. "+
		"If unspecified, Chiron will disable monitoring.")
	flags.BoolVar(&opts.enableProfiling, "enable-profiling", false, "Enabling profiling when monitoring Chiron.")

	// Certificate issuance configuration.
	flags.DurationVar(&opts.certTTL, "cert-ttl", cmd.DefaultWorkloadCertTTL,
		"The TTL of issued certificates.")
	flags.DurationVar(&opts.maxCertTTL, "max-cert-ttl", cmd.DefaultMaxWorkloadCertTTL,
		"The max TTL of issued certificates.")
	flags.Float32Var(&opts.certGracePeriodRatio, "cert-grace-period-ratio",
		cmd.DefaultWorkloadCertGracePeriodRatio, "The certificate rotation grace period, as a ratio of the "+
			"certificate TTL.")
	flags.DurationVar(&opts.certMinGracePeriod, "cert-min-grace-period",
		cmd.DefaultWorkloadMinCertGracePeriod, "The minimum certificate rotation grace period.")
	flags.BoolVar(&opts.pkcs8Keys, "pkcs8-keys", false, "Whether to generate PKCS#8 private keys.")

	// MutatingWebhook configuration
	flags.StringVar(&opts.mutatingWebhookConfigName, "mutating-webhook-config-name", "istio-sidecar-injector",
		"The name of the mutatingwebhookconfiguration resource in Kubernetes. Chiron will manage this resource.")
	flags.StringVar(&opts.mutatingWebhookName, "mutating-webhook-name", "sidecar-injector.istio.io",
		"The name of the webhook in mutatingwebhookconfiguration. Only the specified webhook will be managed by Chiron.")

	// Hide the command line options for the prototype
	_ = flags.MarkHidden("enable-controller")
	_ = flags.MarkHidden("certificate-namespace")
	_ = flags.MarkHidden("kube-config")
	_ = flags.MarkHidden("monitoring-port")
	_ = flags.MarkHidden("enable-profiling")
	_ = flags.MarkHidden("cert-ttl")
	_ = flags.MarkHidden("max-cert-ttl")
	_ = flags.MarkHidden("cert-grace-period-ratio")
	_ = flags.MarkHidden("cert-min-grace-period")
	_ = flags.MarkHidden("pkcs8-keys")
	_ = flags.MarkHidden("mutating-webhook-config-name")
	_ = flags.MarkHidden("mutating-webhook-name")

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

	log.Debug("run webhook controller")

	webhooks := controller.ConstructCustomDNSNames(chiron.WebhookServiceAccounts,
		chiron.WebhookServiceNames, opts.certificateNamespace, "")

	k8sClient, err := kube.CreateClientset(opts.kubeConfigFile, "")
	if err != nil {
		log.Errorf("could not create k8s clientset: %v", err)
		os.Exit(1)
	}

	stopCh := make(chan struct{})

	sc, err := chiron.NewWebhookController(opts.certTTL,
		opts.certGracePeriodRatio, opts.certMinGracePeriod, false,
		k8sClient, k8sClient.CoreV1(), k8sClient.CertificatesV1beta1(), opts.pkcs8Keys, webhooks,
		opts.certificateNamespace, opts.mutatingWebhookConfigName, opts.mutatingWebhookName)
	if err != nil {
		log.Errorf("failed to create webhook controller: %v", err)
		os.Exit(1)
	}

	// Run the controller to manage the lifecycles of webhook certificates and webhook configurations
	sc.Run(stopCh)
	defer sc.CaCertWatcher.Close()

	monitorErrCh := make(chan error)
	// Start the monitoring server.
	if opts.monitoringPort > 0 {
		monitor, mErr := monitoring.NewMonitor(opts.monitoringPort, opts.enableProfiling)
		if mErr != nil {
			fatalf("unable to setup monitoring: %v", mErr)
		}
		go monitor.Start(monitorErrCh)
		log.Info("Chiron monitor has started.")
		defer monitor.Close()
	}

	// Blocking until receives error.
	for _ = range monitorErrCh {
		// TODO: does the controller exit when receiving an error?
		fatalf("monitoring server error: %v", err)
	}
}
