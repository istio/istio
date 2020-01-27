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
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"istio.io/istio/pkg/jwt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgcmd "istio.io/istio/pkg/cmd"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/caclient"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/ca"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/registry/kube"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/monitoring"
	"istio.io/pkg/collateral"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
	"istio.io/pkg/version"
)

const (
	selfSignedCaCertTTL                     = "CITADEL_SELF_SIGNED_CA_CERT_TTL"
	selfSignedRootCertCheckInterval         = "CITADEL_SELF_SIGNED_ROOT_CERT_CHECK_INTERVAL"
	selfSignedRootCertGracePeriodPercentile = "CITADEL_SELF_SIGNED_ROOT_CERT_GRACE_PERIOD_PERCENTILE"
	workloadCertMinGracePeriod              = "CITADEL_WORKLOAD_CERT_MIN_GRACE_PERIOD"
	enableJitterForRootCertRotator          = "CITADEL_ENABLE_JITTER_FOR_ROOT_CERT_ROTATOR"
)

type cliOptions struct { // nolint: maligned
	// Comma separated string containing all listened namespaces
	listenedNamespaces        string
	enableNamespacesByDefault bool
	istioCaStorageNamespace   string
	kubeConfigFile            string
	readSigningCertOnly       bool

	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	selfSignedCA                            bool
	selfSignedCACertTTL                     time.Duration
	selfSignedRootCertCheckInterval         time.Duration
	selfSignedRootCertGracePeriodPercentile int
	enableJitterForRootCertRotator          bool

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

	// Whether SDS is enabled on.
	sdsEnabled bool

	// The policy for validating JWT
	jwtPolicy string
}

var (
	opts = cliOptions{
		loggingOptions:       log.DefaultOptions(),
		ctrlzOptions:         ctrlz.DefaultOptions(),
		LivenessProbeOptions: &probe.Options{},
		selfSignedCACertTTL: env.RegisterDurationVar(selfSignedCaCertTTL,
			cmd.DefaultSelfSignedCACertTTL,
			"The TTL of self-signed CA root certificate.").Get(),
		selfSignedRootCertCheckInterval: env.RegisterDurationVar(selfSignedRootCertCheckInterval,
			cmd.DefaultSelfSignedRootCertCheckInterval,
			"The interval that self-signed CA checks its root certificate "+
				"expiration time and rotates root certificate. Setting this interval "+
				"to zero or a negative value disables automated root cert check and "+
				"rotation. This interval is suggested to be larger than 10 minutes.").Get(),
		selfSignedRootCertGracePeriodPercentile: env.RegisterIntVar(selfSignedRootCertGracePeriodPercentile,
			cmd.DefaultRootCertGracePeriodPercentile,
			"Grace period percentile for self-signed root cert.").Get(),
		workloadCertMinGracePeriod: env.RegisterDurationVar(workloadCertMinGracePeriod,
			cmd.DefaultWorkloadMinCertGracePeriod,
			"The minimum workload certificate rotation grace period.").Get(),
		enableJitterForRootCertRotator: env.RegisterBoolVar(enableJitterForRootCertRotator,
			true,
			"If true, set up a jitter to start root cert rotator. "+
				"Jitter selects a backoff time in seconds to start root cert rotator, "+
				"and the back off time is below root cert check interval.").Get(),
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
	initCLI()
	initEnvVars()
}

func initCLI() {
	flags := rootCmd.Flags()
	// General configuration.
	flags.StringVar(&opts.listenedNamespaces, "listened-namespace", metav1.NamespaceAll, "deprecated")
	if err := flags.MarkDeprecated("listened-namespace", "please use --listened-namespaces instead"); err != nil {
		panic(err)
	}

	// Default to NamespaceAll, which equals to "". Kuberentes library will then watch all the namespace.
	flags.StringVar(&opts.listenedNamespaces, "listened-namespaces", metav1.NamespaceAll,
		"Select the namespaces for the Citadel to listen to, separated by comma. If unspecified, Citadel tries to use the ${"+
			cmd.ListenedNamespaceKey+"} environment variable. If neither is set, Citadel listens to all namespaces.")
	flags.StringVar(&opts.istioCaStorageNamespace, "citadel-storage-namespace", "istio-system", "Namespace where "+
		"the Citadel pod is running. Will not be used if explicit file or other storage mechanism is specified.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")
	flags.BoolVar(&opts.readSigningCertOnly, "read-signing-cert-only", false, "When set, Citadel only reads the self-signed signing "+
		"cert and key from Kubernetes secret without generating one (if not exist). This flag avoids racing condition between "+
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

	flags.BoolVar(&opts.sdsEnabled, "sds-enabled", false, "Whether SDS is enabled.")

	rootCmd.AddCommand(version.CobraCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Citadel",
		Section: "istio_ca CLI",
		Manual:  "Citadel",
	}))

	rootCmd.AddCommand(cmd.NewProbeCmd())

	opts.loggingOptions.AttachCobraFlags(rootCmd)
	opts.ctrlzOptions.AttachCobraFlags(rootCmd)

	pkgcmd.AddFlags(rootCmd)
}

func initEnvVars() {
	enableNamespacesByDefault := env.RegisterBoolVar("CITADEL_ENABLE_NAMESPACES_BY_DEFAULT", true,
		"Determines whether unlabeled namespaces should be targeted by this Citadel instance").Get()
	opts.enableNamespacesByDefault = enableNamespacesByDefault
	jwtPolicy := env.RegisterStringVar("JWT_POLICY", jwt.JWTPolicyThirdPartyJWT,
		"The JWT validation policy.").Get()
	opts.jwtPolicy = jwtPolicy
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
	verifyCommandLineOptions()

	if err := log.Configure(opts.loggingOptions); err != nil {
		fatalf("Failed to configure logging (%v)", err)
	}

	_, _ = ctrlz.Run(opts.ctrlzOptions, nil)

	if value, exists := listenedNamespaceKeyVar.Lookup(); exists {
		// When -namespace is not set, try to read the namespace from environment variable.
		if opts.listenedNamespaces == "" {
			opts.listenedNamespaces = value
		}
	}

	listenedNamespaces := strings.Split(opts.listenedNamespaces, ",")

	var webhooks map[string]*controller.DNSNameEntry
	if opts.appendDNSNames {
		webhooks = controller.ConstructCustomDNSNames(webhookServiceAccounts,
			webhookServiceNames, opts.istioCaStorageNamespace, opts.customDNSNames)
	}

	cs, err := kubelib.CreateClientset(opts.kubeConfigFile, "")
	if err != nil {
		fatalf("Could not create k8s clientset: %v", err)
	}

	stopCh := make(chan struct{})
	ca := createCA(cs.CoreV1(), stopCh)
	if !opts.serverOnly {
		log.Infof("Creating Kubernetes controller to write issued keys and certs into secret ...")
		// For workloads in K8s, we apply the configured workload cert TTL.
		sc, err := controller.NewSecretController(ca, opts.enableNamespacesByDefault,
			opts.workloadCertTTL, opts.workloadCertGracePeriodRatio, opts.workloadCertMinGracePeriod,
			opts.dualUse, cs.CoreV1(), opts.signCACerts, opts.pkcs8Keys, listenedNamespaces, webhooks,
			opts.istioCaStorageNamespace, opts.rootCertFile, opts.selfSignedCA)
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
			log.Errorf("Failed to add identity mapping: %v", registryErr)
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
		caServer, startErr := caserver.New(ca, opts.maxWorkloadCertTTL,
			opts.signCACerts, hostnames, opts.grpcPort, spiffe.GetTrustDomain(),
			opts.sdsEnabled, opts.jwtPolicy)
		if startErr != nil {
			fatalf("Failed to create istio ca server: %v", startErr)
		}
		if serverErr := caServer.Run(); serverErr != nil {
			// stop the registry-related controllers
			ch <- struct{}{}

			log.Warnf("Failed to start GRPC server with error: %v", serverErr)
		}
	}

	monitorErrCh := make(chan error, 10)
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

	rotatorErrCh := make(chan error, 10)
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

	// Capture termination and close the stop channel
	go pkgcmd.WaitSignal(stopCh)

	// Blocking until receives error.
	select {
	case <-stopCh:
		log.Infof("Stopping CA server, termination signal received")
		return
	case err := <-monitorErrCh:
		fatalf("Monitoring server error: %v", err)
	case err := <-rotatorErrCh:
		fatalf("Key cert bundle rotator error: %v", err)
	}
}

func createCA(client corev1.CoreV1Interface, stopCh chan struct{}) *ca.IstioCA {
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
			checkInterval = cmd.ReadSigningCertRetryInterval
		} else {
			checkInterval = -1
		}
		caOpts, err = ca.NewSelfSignedIstioCAOptions(ctx,
			opts.selfSignedRootCertGracePeriodPercentile, opts.selfSignedCACertTTL,
			opts.selfSignedRootCertCheckInterval, opts.workloadCertTTL,
			opts.maxWorkloadCertTTL, spiffe.GetTrustDomain(), opts.dualUse,
			opts.istioCaStorageNamespace, checkInterval, client, opts.rootCertFile,
			opts.enableJitterForRootCertRotator)
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
	// Start root cert rotator in a separate goroutine.
	istioCA.Run(stopCh)

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
