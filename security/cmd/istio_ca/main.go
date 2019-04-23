// Copyright 2017 Istio Authors
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
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/env"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/pkg/caclient"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/ca"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/registry/kube"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/monitoring"
)

type cliOptions struct { // nolint: maligned
	// Comma separated string containing all listened namespaces
	listenedNamespaces      string
	istioCaStorageNamespace string
	kubeConfigFile          string
	readSigningCertOnly     bool

	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	selfSignedCA        bool
	selfSignedCACertTTL time.Duration

	// if set, namespaces require explicit labeling to have Citadel generate secrets.
	explicitOptInRequired bool

	workloadCertTTL    time.Duration
	maxWorkloadCertTTL time.Duration
	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	// If workloadCertGracePeriodRatio is 0.2, and cert TTL is 24 hours, then the rotation will happen
	// after 24*(1-0.2) hours since the cert is issued.
	workloadCertGracePeriodRatio float32
	// The minimum grace period for workload cert rotation.
	workloadCertMinGracePeriod time.Duration

	// Comma separated string containing all possible host name that clients may use to connect to.
	grpcHosts  string
	grpcPort   int
	serverOnly bool

	// Whether the CA signs certificates for other CAs.
	signCACerts bool
	// Whether to generate PKCS#8 private keys.
	pkcs8Keys bool

	cAClientConfig caclient.Config

	// Monitoring port number
	monitoringPort int
	// Enable profiling in monitoring
	enableProfiling bool

	// The path to the file which indicates the liveness of the server by its existence.
	// This will be used for k8s liveness probe. If empty, it does nothing.
	LivenessProbeOptions *probe.Options
	probeCheckInterval   time.Duration

	loggingOptions *log.Options
	ctrlzOptions   *ctrlz.Options

	// Whether to append DNS names to the certificate
	appendDNSNames bool

	// Custom domain options, for control plane and special service accounts
	// comma separated list of SERVICE_ACCOUNT.NAMESPACE:DOMAIN
	customDNSNames string

	// domain to use in SPIFFE identity URLs
	trustDomain string

	// Enable dual-use certs - SPIFFE in SAN and in CommonName
	dualUse bool
}

var (
	opts = cliOptions{
		loggingOptions:       log.DefaultOptions(),
		ctrlzOptions:         ctrlz.DefaultOptions(),
		LivenessProbeOptions: &probe.Options{},
	}

	rootCmd = &cobra.Command{
		Use:   "istio_ca",
		Short: "Istio Certificate Authority (CA).",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			runCA()
		},
	}
	// ServiceAccount/DNS pair for generating DNS names in certificates.
	// TODO: move it to a configmap later when we have more services to support.
	webhookServiceAccounts = []string{
		"istio-sidecar-injector-service-account",
		"istio-galley-service-account",
	}
	webhookServiceNames = []string{
		"istio-sidecar-injector",
		"istio-galley",
	}
)

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
	// General configuration.
	flags.StringVar(&opts.listenedNamespaces, "listened-namespace", "", "deprecated")
	if err := flags.MarkDeprecated("listened-namespace", "please use --listened-namespaces instead"); err != nil {
		panic(err)
	}

	flags.StringVar(&opts.listenedNamespaces, "listened-namespaces", "",
		"Select the namespaces for the Citadel to listen to, separated by comma. If unspecified, Citadel tries to use the ${"+
			cmd.ListenedNamespaceKey+"} environment variable. If neither is set, Citadel listens to all namespaces.")
	flags.StringVar(&opts.istioCaStorageNamespace, "citadel-storage-namespace", "istio-system", "Namespace where "+
		"the Citadel pod is running. Will not be used if explicit file or other storage mechanism is specified.")
	flags.BoolVar(&opts.explicitOptInRequired, "explicit-opt-in", false, "Specifies whether Citadel requires "+
		"explicit opt-in for creating secrets. If set, only namespaces labeled with 'istio-managed=enabled' will "+
		"have secrets created. This feature is only available in key and certificates delivered through secret volume mount.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")
	flags.BoolVar(&opts.readSigningCertOnly, "read-signing-cert-only", false, "When set, Citadel only reads the self-signed signing "+
		"key and cert from Kubernetes secret without generating one (if not exist). This flag avoids racing condition between "+
		"multiple Citadels generating self-signed key and cert. Please make sure one and only one Citadel instance has this flag set "+
		"to false.")

	// Configuration if Citadel accepts key/cert configured through arguments.
	flags.StringVar(&opts.certChainFile, "cert-chain", "", "Path to the certificate chain file.")
	flags.StringVar(&opts.signingCertFile, "signing-cert", "", "Path to the CA signing certificate file.")
	flags.StringVar(&opts.signingKeyFile, "signing-key", "", "Path to the CA signing key file.")

	// Both self-signed or non-self-signed Citadel may take a root certificate file with a list of root certificates.
	flags.StringVar(&opts.rootCertFile, "root-cert", "", "Path to the root certificate file.")

	// Configuration if Citadel acts as a self signed CA.
	flags.BoolVar(&opts.selfSignedCA, "self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '--signing-cert' and '--signing-key' options are ignored.")
	flags.DurationVar(&opts.selfSignedCACertTTL, "self-signed-ca-cert-ttl", cmd.DefaultSelfSignedCACertTTL,
		"The TTL of self-signed CA root certificate.")
	flags.StringVar(&opts.trustDomain, "trust-domain", "",
		"The domain serves to identify the system with SPIFFE.")
	// Upstream CA configuration if Citadel interacts with upstream CA.
	flags.StringVar(&opts.cAClientConfig.CAAddress, "upstream-ca-address", "", "The IP:port address of the upstream "+
		"CA. When set, the CA will rely on the upstream Citadel to provision its own certificate.")
	flags.StringVar(&opts.cAClientConfig.Org, "org", "", "Organization for the certificate.")
	flags.DurationVar(&opts.cAClientConfig.RequestedCertTTL, "requested-ca-cert-ttl", cmd.DefaultRequestedCACertTTL,
		"The requested TTL for the CA certificate.")
	flags.IntVar(&opts.cAClientConfig.RSAKeySize, "key-size", 2048, "Size of generated private key.")

	// Certificate signing configuration.
	flags.DurationVar(&opts.workloadCertTTL, "workload-cert-ttl", cmd.DefaultWorkloadCertTTL,
		"The TTL of issued workload certificates.")
	flags.DurationVar(&opts.maxWorkloadCertTTL, "max-workload-cert-ttl", cmd.DefaultMaxWorkloadCertTTL,
		"The max TTL of issued workload certificates.")
	flags.Float32Var(&opts.workloadCertGracePeriodRatio, "workload-cert-grace-period-ratio",
		cmd.DefaultWorkloadCertGracePeriodRatio, "The workload certificate rotation grace period, as a ratio of the "+
			"workload certificate TTL.")
	flags.DurationVar(&opts.workloadCertMinGracePeriod, "workload-cert-min-grace-period",
		cmd.DefaultWorkloadMinCertGracePeriod, "The minimum workload certificate rotation grace period.")

	// gRPC server for signing CSRs.
	flags.StringVar(&opts.grpcHosts, "grpc-host-identities", "istio-ca,istio-citadel",
		"The list of hostnames for istio ca server, separated by comma.")
	flags.IntVar(&opts.grpcPort, "grpc-port", 8060, "The port number for Citadel GRPC server. "+
		"If unspecified, Citadel will not serve GRPC requests.")
	flags.BoolVar(&opts.serverOnly, "server-only", false, "When set, Citadel only serves as a server without writing "+
		"the Kubernetes secrets.")

	flags.BoolVar(&opts.signCACerts, "sign-ca-certs", false, "Whether Citadel signs certificates for other CAs.")
	flags.BoolVar(&opts.pkcs8Keys, "pkcs8-keys", false, "Whether to generate PKCS#8 private keys.")

	// Monitoring configuration
	flags.IntVar(&opts.monitoringPort, "monitoring-port", 15014, "The port number for monitoring Citadel. "+
		"If unspecified, Citadel will disable monitoring.")
	flags.BoolVar(&opts.enableProfiling, "enable-profiling", false, "Enabling profiling when monitoring Citadel.")

	// Liveness Probe configuration
	flags.StringVar(&opts.LivenessProbeOptions.Path, "liveness-probe-path", "",
		"Path to the file for the liveness probe.")
	flags.DurationVar(&opts.LivenessProbeOptions.UpdateInterval, "liveness-probe-interval", 0,
		"Interval of updating file for the liveness probe.")
	flags.DurationVar(&opts.probeCheckInterval, "probe-check-interval", cmd.DefaultProbeCheckInterval,
		"Interval of checking the liveness of the CA.")

	flags.BoolVar(&opts.appendDNSNames, "append-dns-names", true,
		"Append DNS names to the certificates for webhook services.")
	flags.StringVar(&opts.customDNSNames, "custom-dns-names", "",
		"The list of account.namespace:customdns names, separated by comma.")

	// Dual-use certificate signing
	flags.BoolVar(&opts.dualUse, "experimental-dual-use",
		false, "Enable dual-use mode. Generates certificates with a CommonName identical to the SAN.")

	rootCmd.AddCommand(version.CobraCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Citadel",
		Section: "istio_ca CLI",
		Manual:  "Citadel",
	}))

	rootCmd.AddCommand(cmd.NewProbeCmd())

	opts.loggingOptions.AttachCobraFlags(rootCmd)
	opts.ctrlzOptions.AttachCobraFlags(rootCmd)

	cmd.InitializeFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}

// fqdn returns the k8s cluster dns name for the Citadel service.
func fqdn() string {
	return fmt.Sprintf("istio-citadel.%v.svc.cluster.local", opts.istioCaStorageNamespace)
}

var (
	listenedNamespaceKeyVar = env.RegisterStringVar(cmd.ListenedNamespaceKey, "", "")
)

func runCA() {
	if err := log.Configure(opts.loggingOptions); err != nil {
		fatalf("Failed to configure logging (%v)", err)
	}

	_, _ = ctrlz.Run(opts.ctrlzOptions, nil)

	if value, exists := listenedNamespaceKeyVar.Lookup(); exists {
		// When -namespace is not set, try to read the namespace from environment variable.
		if opts.listenedNamespaces == "" {
			opts.listenedNamespaces = value
		}
		// Use environment variable for istioCaStorageNamespace if it exists
		opts.istioCaStorageNamespace = value
	}

	listenedNamespaces := strings.Split(opts.listenedNamespaces, ",")

	verifyCommandLineOptions()

	var webhooks map[string]*controller.DNSNameEntry
	if opts.appendDNSNames {
		webhooks = controller.ConstructCustomDNSNames(webhookServiceAccounts,
			webhookServiceNames, opts.istioCaStorageNamespace, opts.customDNSNames)
	}

	cs, err := kubelib.CreateClientset(opts.kubeConfigFile, "")
	if err != nil {
		fatalf("Could not create k8s clientset: %v", err)
	}
	ca := createCA(cs.CoreV1())

	stopCh := make(chan struct{})
	if !opts.serverOnly {
		log.Infof("Creating Kubernetes controller to write issued keys and certs into secret ...")
		// For workloads in K8s, we apply the configured workload cert TTL.
		sc, err := controller.NewSecretController(ca, opts.explicitOptInRequired,
			opts.workloadCertTTL,
			opts.workloadCertGracePeriodRatio, opts.workloadCertMinGracePeriod, opts.dualUse,
			cs.CoreV1(), opts.signCACerts, opts.pkcs8Keys, listenedNamespaces, webhooks)
		if err != nil {
			fatalf("Failed to create secret controller: %v", err)
		}
		sc.Run(stopCh)
	} else {
		log.Info("Citadel is running in server only mode, certificates will not be propagated via secret.")
		if opts.grpcPort <= 0 {
			fatalf("The gRPC port must be set in server-only model.")
		}
	}

	if opts.grpcPort > 0 {
		// start registry if gRPC server is to be started
		reg := registry.GetIdentityRegistry()

		// add certificate identity to the identity registry for the liveness probe check
		if registryErr := reg.AddMapping(probecontroller.LivenessProbeClientIdentity,
			probecontroller.LivenessProbeClientIdentity); registryErr != nil {
			log.Errorf("Failed to add indentity mapping: %v", registryErr)
		}

		ch := make(chan struct{})

		// monitor service objects with "alpha.istio.io/kubernetes-serviceaccounts" and
		// "alpha.istio.io/canonical-serviceaccounts" annotations
		serviceController := kube.NewServiceController(cs.CoreV1(), listenedNamespaces, reg)
		serviceController.Run(ch)

		// monitor service account objects for istio mesh expansion
		serviceAccountController := kube.NewServiceAccountController(cs.CoreV1(), listenedNamespaces, reg)
		serviceAccountController.Run(ch)

		// The CA API uses cert with the max workload cert TTL.
		hostnames := append(strings.Split(opts.grpcHosts, ","), fqdn())
		caServer, startErr := caserver.New(ca, opts.maxWorkloadCertTTL, opts.signCACerts, hostnames, opts.grpcPort, spiffe.GetTrustDomain())
		if startErr != nil {
			fatalf("Failed to create istio ca server: %v", startErr)
		}
		if serverErr := caServer.Run(); serverErr != nil {
			// stop the registry-related controllers
			ch <- struct{}{}

			log.Warnf("Failed to start GRPC server with error: %v", serverErr)
		}
	}

	monitorErrCh := make(chan error)
	// Start the monitoring server.
	if opts.monitoringPort > 0 {
		monitor, mErr := monitoring.NewMonitor(opts.monitoringPort, opts.enableProfiling)
		if mErr != nil {
			fatalf("Unable to setup monitoring: %v", mErr)
		}
		go monitor.Start(monitorErrCh)
		log.Info("Citadel monitor has started.")
		defer monitor.Close()
	}

	log.Info("Citadel has started")

	rotatorErrCh := make(chan error)
	// Start CA client if the upstream CA address is specified.
	if len(opts.cAClientConfig.CAAddress) != 0 {
		config := &opts.cAClientConfig
		config.Env = "onprem"
		config.Platform = "vm"
		config.ForCA = true
		config.CertFile = opts.signingCertFile
		config.KeyFile = opts.signingKeyFile
		config.CertChainFile = opts.certChainFile
		config.RootCertFile = opts.rootCertFile
		config.CSRGracePeriodPercentage = cmd.DefaultCSRGracePeriodPercentage
		config.CSRMaxRetries = cmd.DefaultCSRMaxRetries
		config.CSRInitialRetrialInterval = cmd.DefaultCSRInitialRetrialInterval
		rotator, creationErr := caclient.NewKeyCertBundleRotator(config, ca.GetCAKeyCertBundle())
		if creationErr != nil {
			fatalf("Failed to create key cert bundle rotator: %v", creationErr)
		}
		go rotator.Start(rotatorErrCh)
		log.Info("Key cert bundle rotator has started.")
		defer rotator.Stop()
	}

	// Blocking until receives error.
	for {
		select {
		case <-monitorErrCh:
			fatalf("Monitoring server error: %v", err)
		case <-rotatorErrCh:
			fatalf("Key cert bundle rotator error: %v", err)
		}
	}
}

func createCA(client corev1.CoreV1Interface) *ca.IstioCA {
	var caOpts *ca.IstioCAOptions
	var err error

	if opts.selfSignedCA {
		log.Info("Use self-signed certificate as the CA certificate")
		spiffe.SetTrustDomain(spiffe.DetermineTrustDomain(opts.trustDomain, true))
		// Abort after 20 minutes.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*20)
		defer cancel()
		var checkInterval time.Duration
		if opts.readSigningCertOnly {
			checkInterval = ca.ReadSigningCertCheckInterval
		} else {
			checkInterval = -1
		}
		caOpts, err = ca.NewSelfSignedIstioCAOptions(ctx, opts.selfSignedCACertTTL, opts.workloadCertTTL,
			opts.maxWorkloadCertTTL, spiffe.GetTrustDomain(), opts.dualUse,
			opts.istioCaStorageNamespace, checkInterval, client, opts.rootCertFile)
		if err != nil {
			fatalf("Failed to create a self-signed Citadel (error: %v)", err)
		}
	} else {
		log.Info("Use certificate from argument as the CA certificate")
		caOpts, err = ca.NewPluggedCertIstioCAOptions(opts.certChainFile, opts.signingCertFile, opts.signingKeyFile,
			opts.rootCertFile, opts.workloadCertTTL, opts.maxWorkloadCertTTL, opts.istioCaStorageNamespace, client)
		if err != nil {
			fatalf("Failed to create an Citadel (error: %v)", err)
		}
	}

	caOpts.LivenessProbeOptions = opts.LivenessProbeOptions
	caOpts.ProbeCheckInterval = opts.probeCheckInterval

	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		log.Errorf("Failed to create an Citadel (error: %v)", err)
	}

	if opts.LivenessProbeOptions.IsValid() {
		livenessProbeChecker, err := probecontroller.NewLivenessCheckController(
			opts.probeCheckInterval, fmt.Sprintf("%v:%v", fqdn(), opts.grpcPort), istioCA,
			opts.LivenessProbeOptions, probecontroller.GrpcProtocolProvider)
		if err != nil {
			log.Errorf("failed to create an liveness probe check controller (error: %v)", err)
		} else {
			livenessProbeChecker.Run()
		}
	}

	return istioCA
}

func verifyCommandLineOptions() {
	if opts.selfSignedCA {
		return
	}

	if opts.signingCertFile == "" {
		fatalf(
			"No signing cert has been specified. Either specify a cert file via '-signing-cert' option " +
				"or use '-self-signed-ca'")
	}

	if opts.signingKeyFile == "" {
		fatalf(
			"No signing key has been specified. Either specify a key file via '-signing-key' option " +
				"or use '-self-signed-ca'")
	}

	if opts.rootCertFile == "" {
		fatalf(
			"No root cert has been specified. Either specify a root cert file via '-root-cert' option " +
				"or use '-self-signed-ca'")
	}

	if opts.workloadCertGracePeriodRatio < 0 || opts.workloadCertGracePeriodRatio > 1 {
		fatalf("Workload cert grace period ratio %f is invalid. It should be within [0, 1]",
			opts.workloadCertGracePeriodRatio)
	}
}
