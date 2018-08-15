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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/pkg/caclient"
	"istio.io/istio/security/pkg/caclient/protocol"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ca/controller"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/registry/kube"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/monitoring"
)

// TODO(myidpt): move the following constants to pkg/cmd.
const (
	defaultSelfSignedCACertTTL = 365 * 24 * time.Hour

	defaultRequestedCACertTTL = 365 * 24 * time.Hour

	defaultMaxWorkloadCertTTL = 90 * 24 * time.Hour

	defaultWorkloadCertTTL = 90 * 24 * time.Hour

	// The default length of certificate rotation grace period, configured as
	// the ratio of the certificate TTL.
	defaultWorkloadCertGracePeriodRatio = 0.5

	// The default minimum grace period for workload cert rotation.
	defaultWorkloadMinCertGracePeriod = 10 * time.Minute

	defaultProbeCheckInterval = 30 * time.Second

	// The default length of certificate rotation grace period, configured as
	// the percentage of the certificate TTL.
	defaultCSRGracePeriodPercentage = 50

	// The default initial interval between retries to send CSR to upstream CA.
	defaultCSRInitialRetrialInterval = time.Second

	// The default value of CSR retries for Citadel to send CSR to upstream CA.
	defaultCSRMaxRetries = 10

	// The key for the environment variable that specifies the namespace.
	listenedNamespaceKey = "NAMESPACE"

	// The default SPIFFE URL value for identity domain
	defaultIdentityDomain = "cluster.local"
)

type keyCertBundleRotator interface {
	Start(chan<- error)
	Stop()
}

type cliOptions struct { // nolint: maligned
	listenedNamespace       string
	istioCaStorageNamespace string
	kubeConfigFile          string

	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	selfSignedCA        bool
	selfSignedCACertTTL time.Duration

	workloadCertTTL    time.Duration
	maxWorkloadCertTTL time.Duration
	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	workloadCertGracePeriodRatio float32
	// The minimum grace period for workload cert rotation.
	workloadCertMinGracePeriod time.Duration

	// TODO(incfly): delete this field once we deprecate flag --grpc-hostname.
	grpcHostname string
	// Comma separated string containing all possible host name that clients may use to connect to.
	grpcHosts string
	grpcPort  int

	// Whether the CA signs certificates for other CAs.
	signCACerts bool

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
	identityDomain string

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
		Short: "Istio Certificate Authority (CA)",
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
		log.Errorf(template, args)
	} else {
		log.Errorf(template)
	}
	os.Exit(-1)
}

func init() {
	flags := rootCmd.Flags()
	// General configuration.
	flags.StringVar(&opts.listenedNamespace, "listened-namespace", "",
		"Select a namespace for the CA to listen to. If unspecified, Citadel tries to use the ${"+
			listenedNamespaceKey+"} environment variable. If neither is set, Citadel listens to all namespaces.")
	flags.StringVar(&opts.istioCaStorageNamespace, "citadel-storage-namespace", "istio-system", "Namespace where "+
		"the Citadel pod is running. Will not be used if explicit file or other storage mechanism is specified.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	// Configuration if Citadel accepts key/cert configured through arguments.
	flags.StringVar(&opts.certChainFile, "cert-chain", "", "Path to the certificate chain file")
	flags.StringVar(&opts.signingCertFile, "signing-cert", "", "Path to the CA signing certificate file")
	flags.StringVar(&opts.signingKeyFile, "signing-key", "", "Path to the CA signing key file")
	flags.StringVar(&opts.rootCertFile, "root-cert", "", "Path to the root certificate file")

	// Configuration if Citadel acts as a self signed CA.
	flags.BoolVar(&opts.selfSignedCA, "self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '--signing-cert' and '--signing-key' options are ignored.")
	flags.DurationVar(&opts.selfSignedCACertTTL, "self-signed-ca-cert-ttl", defaultSelfSignedCACertTTL,
		"The TTL of self-signed CA root certificate")
	flags.StringVar(&opts.identityDomain, "identity-domain", controller.DefaultIdentityDomain,
		fmt.Sprintf("The domain to use for identities (default: %s)", controller.DefaultIdentityDomain))

	// Upstream CA configuration if Citadel interacts with upstream CA.
	flags.StringVar(&opts.cAClientConfig.CAAddress, "upstream-ca-address", "", "The IP:port address of the upstream "+
		"CA. When set, the CA will rely on the upstream Citadel to provision its own certificate.")
	flags.StringVar(&opts.cAClientConfig.Org, "org", "", "Organization for the cert")
	flags.DurationVar(&opts.cAClientConfig.RequestedCertTTL, "requested-ca-cert-ttl", defaultRequestedCACertTTL,
		"The requested TTL for the workload")
	flags.IntVar(&opts.cAClientConfig.RSAKeySize, "key-size", 2048, "Size of generated private key")

	// Certificate signing configuration.
	flags.DurationVar(&opts.workloadCertTTL, "workload-cert-ttl", defaultWorkloadCertTTL,
		"The TTL of issued workload certificates")
	flags.DurationVar(&opts.maxWorkloadCertTTL, "max-workload-cert-ttl", defaultMaxWorkloadCertTTL,
		"The max TTL of issued workload certificates")
	flags.Float32Var(&opts.workloadCertGracePeriodRatio, "workload-cert-grace-period-ratio",
		defaultWorkloadCertGracePeriodRatio, "The workload certificate rotation grace period, as a ratio of the "+
			"workload certificate TTL.")
	flags.DurationVar(&opts.workloadCertMinGracePeriod, "workload-cert-min-grace-period",
		defaultWorkloadMinCertGracePeriod, "The minimum workload certificate rotation grace period.")

	// gRPC server for signing CSRs.
	flags.StringVar(&opts.grpcHostname, "grpc-hostname", "istio-ca", "DEPRECATED, use --grpc-host-identities.")
	flags.StringVar(&opts.grpcHosts, "grpc-host-identities", "istio-ca,istio-citadel",
		"The list of hostnames for istio ca server, separated by comma.")
	flags.IntVar(&opts.grpcPort, "grpc-port", 8060, "The port number for Citadel GRPC server. "+
		"If unspecified, Citadel will not serve GRPC requests.")

	flags.BoolVar(&opts.signCACerts, "sign-ca-certs", false, "Whether Citadel signs certificates for other CAs")

	// Monitoring configuration
	flags.IntVar(&opts.monitoringPort, "monitoring-port", 9093, "The port number for monitoring Citadel. "+
		"If unspecified, Citadel will disable monitoring.")
	flags.BoolVar(&opts.enableProfiling, "enable-profiling", false, "Enabling profiling when monitoring Citadel.")

	// Liveness Probe configuration
	flags.StringVar(&opts.LivenessProbeOptions.Path, "liveness-probe-path", "",
		"Path to the file for the liveness probe.")
	flags.DurationVar(&opts.LivenessProbeOptions.UpdateInterval, "liveness-probe-interval", 0,
		"Interval of updating file for the liveness probe.")
	flags.DurationVar(&opts.probeCheckInterval, "probe-check-interval", defaultProbeCheckInterval,
		"Interval of checking the liveness of the CA.")

	flags.BoolVar(&opts.appendDNSNames, "append-dns-names", true,
		"Append DNS names to the certificates for webhook services.")
	flags.StringVar(&opts.customDNSNames, "custom-dns-names", "",
		"The list of account.namespace:customdns names, separated by comma.")

	// Dual-use certficate signing
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

func runCA() {
	if err := log.Configure(opts.loggingOptions); err != nil {
		fatalf("Failed to configure logging (%v)", err)
	}

	_, _ = ctrlz.Run(opts.ctrlzOptions, nil)

	if value, exists := os.LookupEnv(listenedNamespaceKey); exists {
		// When -namespace is not set, try to read the namespace from environment variable.
		if opts.listenedNamespace == "" {
			opts.listenedNamespace = value
		}
		// Use environment variable for istioCaStorageNamespace if it exists
		opts.istioCaStorageNamespace = value
	}

	verifyCommandLineOptions()

	var webhooks map[string]controller.DNSNameEntry
	if opts.appendDNSNames {
		webhooks = make(map[string]controller.DNSNameEntry)
		for i, svcAccount := range webhookServiceAccounts {
			webhooks[svcAccount] = controller.DNSNameEntry{
				ServiceName: webhookServiceNames[i],
				Namespace:   opts.istioCaStorageNamespace,
			}
		}
		if len(opts.customDNSNames) > 0 {
			customNames := strings.Split(opts.customDNSNames, ",")
			for _, customName := range customNames {
				nameDomain := strings.Split(customName, ":")
				if len(nameDomain) == 2 {
					override, ok := webhooks[nameDomain[0]]
					if ok {
						override.CustomDomains = append(override.CustomDomains, nameDomain[1])
					} else {
						webhooks[nameDomain[0]] = controller.DNSNameEntry{
							ServiceName:   nameDomain[0],
							CustomDomains: []string{nameDomain[1]},
						}
					}
				}
			}
		}
	}

	cs := createClientset()
	ca := createCA(cs.CoreV1())
	// For workloads in K8s, we apply the configured workload cert TTL.
	sc, err := controller.NewSecretController(ca,
		opts.workloadCertTTL, opts.identityDomain,
    opts.workloadCertGracePeriodRatio, opts.workloadCertMinGracePeriod, opts.dualUse,
		cs.CoreV1(), opts.signCACerts, opts.listenedNamespace, webhooks)
	if err != nil {
		fatalf("Failed to create secret controller: %v", err)
	}

	stopCh := make(chan struct{})
	sc.Run(stopCh)

	if opts.grpcPort > 0 {
		// start registry if gRPC server is to be started
		reg := registry.GetIdentityRegistry()

		// add certificate identity to the identity registry for the liveness probe check
		if registryErr := reg.AddMapping(probecontroller.LivenessProbeClientIdentity,
			probecontroller.LivenessProbeClientIdentity); registryErr != nil {
			log.Errorf("Failed to add indentity mapping: %v", registryErr)
		}

		ch := make(chan struct{})

		// monitor service objects with "alpha.istio.io/kubernetes-serviceaccounts" annotation
		serviceController := kube.NewServiceController(cs.CoreV1(), opts.listenedNamespace, reg)
		serviceController.Run(ch)

		// monitor service account objects for istio mesh expansion
		serviceAccountController := kube.NewServiceAccountController(cs.CoreV1(), opts.listenedNamespace, reg)
		serviceAccountController.Run(ch)

		// The CA API uses cert with the max workload cert TTL.
		hostnames := append(strings.Split(opts.grpcHosts, ","), fqdn())
		caServer, startErr := caserver.New(ca, opts.maxWorkloadCertTTL, opts.signCACerts, hostnames, opts.grpcPort)
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
		rotator, creationErr := createKeyCertBundleRotator(ca.GetCAKeyCertBundle())
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

func createClientset() *kubernetes.Clientset {
	c := generateConfig()
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		fatalf("Failed to create a clientset (error: %s)", err)
	}
	return cs
}

func createCA(core corev1.SecretsGetter) *ca.IstioCA {
	var caOpts *ca.IstioCAOptions
	var err error

	if opts.selfSignedCA {
		log.Info("Use self-signed certificate as the CA certificate")
		caOpts, err = ca.NewSelfSignedIstioCAOptions(opts.selfSignedCACertTTL, opts.workloadCertTTL,
			opts.maxWorkloadCertTTL, opts.identityDomain, opts.dualUse,
			opts.istioCaStorageNamespace, core)
		if err != nil {
			fatalf("Failed to create a self-signed Citadel (error: %v)", err)
		}
	} else {
		log.Info("Use certificate from argument as the CA certificate")
		caOpts, err = ca.NewPluggedCertIstioCAOptions(opts.certChainFile, opts.signingCertFile, opts.signingKeyFile,
			opts.rootCertFile, opts.workloadCertTTL, opts.maxWorkloadCertTTL)
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

func generateConfig() *rest.Config {
	c, err := clientcmd.BuildConfigFromFlags("", opts.kubeConfigFile)
	if err != nil {
		fatalf("Failed to create a config (error: %s)", err)
	}
	return c
}

func createKeyCertBundleRotator(keycert pkiutil.KeyCertBundle) (keyCertBundleRotator, error) {
	config := &opts.cAClientConfig
	// Currently cluster CA needs key/cert to talk to upstream CA.
	config.Env = "onprem"
	config.Platform = "vm"
	config.ForCA = true
	config.CertFile = opts.signingCertFile
	config.KeyFile = opts.signingKeyFile
	config.CertChainFile = opts.certChainFile
	config.RootCertFile = opts.rootCertFile
	config.CSRGracePeriodPercentage = defaultCSRGracePeriodPercentage
	config.CSRMaxRetries = defaultCSRMaxRetries
	config.CSRInitialRetrialInterval = defaultCSRInitialRetrialInterval
	pc, err := platform.NewClient(config.Env, config.RootCertFile, config.KeyFile, config.CertChainFile, config.CAAddress)
	if err != nil {
		return nil, err
	}
	dial, err := pc.GetDialOptions()
	if err != nil {
		return nil, err
	}
	grpcConn, err := protocol.NewGrpcConnection(config.CAAddress, dial)
	if err != nil {
		return nil, err
	}
	cac, err := caclient.NewCAClient(pc, grpcConn, config.CSRMaxRetries, config.CSRInitialRetrialInterval)
	if err != nil {
		return nil, err
	}
	return caclient.NewKeyCertBundleRotator(config, cac, keycert)
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
