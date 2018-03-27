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
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
	caclient "istio.io/istio/security/pkg/caclient/grpc"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ca/controller"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/registry/kube"
	"istio.io/istio/security/pkg/server/grpc"
)

const (
	defaultSelfSignedCACertTTL = 365 * 24 * time.Hour

	defaultMaxWorkloadCertTTL = 7 * 24 * time.Hour

	defaultWorkloadCertTTL = 19 * time.Hour

	// The default length of certificate rotation grace period, configured as
	// the ratio of the certificate TTL.
	defaultWorkloadCertGracePeriodRatio = 0.5

	// The default minimum grace period for workload cert rotation.
	defaultWorkloadMinCertGracePeriod = 10 * time.Minute

	defaultProbeCheckInterval = 30 * time.Second

	// The default issuer organization for self-signed CA certificate.
	selfSignedCAOrgDefault = "k8s.cluster.local"

	// The key for the environment variable that specifies the namespace.
	listenedNamespaceKey = "NAMESPACE"
)

type cliOptions struct { // nolint: maligned
	listenedNamespace       string
	istioCaStorageNamespace string
	kubeConfigFile          string

	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	selfSignedCA        bool
	selfSignedCAOrg     string
	selfSignedCACertTTL time.Duration

	workloadCertTTL    time.Duration
	maxWorkloadCertTTL time.Duration
	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	workloadCertGracePeriodRatio float32
	// The minimum grace period for workload cert rotation.
	workloadCertMinGracePeriod time.Duration

	grpcHostname string
	grpcPort     int

	upstreamCAAddress  string
	upstreamCACertFile string
	upstreamAuth       string

	// The path to the file which indicates the liveness of the server by its existence.
	// This will be used for k8s liveness probe. If empty, it does nothing.
	LivenessProbeOptions *probe.Options
	probeCheckInterval   time.Duration

	loggingOptions *log.Options

	// Whether to append DNS names to the certificate
	appendDNSNames bool
}

var (
	opts = cliOptions{
		loggingOptions:       log.DefaultOptions(),
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
		"istio-mixer-validator-service-account",
		"istio-pilot-service-account",
	}
	webhookServiceNames = []string{
		"istio-sidecar-injector",
		"istio-mixer-validator",
		"istio-pilot",
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
		"Select a namespace for the CA to listen to. If unspecified, Istio CA tries to use the ${"+
			listenedNamespaceKey+"} environment variable. If neither is set, Istio CA listens to all namespaces.")
	flags.StringVar(&opts.istioCaStorageNamespace, "istio-ca-storage-namespace", "istio-system", "Namespace where "+
		"the Istio CA pods is running. Will not be used if explicit file or other storage mechanism is specified.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	// Istio CA accepts key/cert configured through arguments.
	flags.StringVar(&opts.certChainFile, "cert-chain", "", "Path to the certificate chain file")
	flags.StringVar(&opts.signingCertFile, "signing-cert", "", "Path to the CA signing certificate file")
	flags.StringVar(&opts.signingKeyFile, "signing-key", "", "Path to the CA signing key file")
	flags.StringVar(&opts.rootCertFile, "root-cert", "", "Path to the root certificate file")

	// Istio CA acts as a self signed CA.
	flags.BoolVar(&opts.selfSignedCA, "self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '--signing-cert' and '--signing-key' options are ignored.")
	flags.StringVar(&opts.selfSignedCAOrg, "self-signed-ca-org", "k8s.cluster.local",
		fmt.Sprintf("The issuer organization used in self-signed CA certificate (default to %s)",
			selfSignedCAOrgDefault))
	flags.DurationVar(&opts.selfSignedCACertTTL, "self-signed-ca-cert-ttl", defaultSelfSignedCACertTTL,
		"The TTL of self-signed CA root certificate")

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
	flags.StringVar(&opts.grpcHostname, "grpc-hostname", "localhost", "The hostname for GRPC server.")
	flags.IntVar(&opts.grpcPort, "grpc-port", 0, "The port number for GRPC server. "+
		"If unspecified, Istio CA will not server GRPC request.")

	// Upstream CA configuration
	flags.StringVar(&opts.upstreamCAAddress, "upstream-ca-address", "", "The IP:port address of the upstream CA. "+
		"When set, the CA will rely on the upstream Istio CA to provision its own certificate.")
	flags.StringVar(&opts.upstreamCACertFile, "upstream-ca-cert-file", "",
		"Path to the certificate for authenticating upstream CA.")
	flags.StringVar(&opts.upstreamAuth, "upstream-auth", "mtls",
		"Specifies how the Istio CA is authenticated to the upstream CA.")

	// Liveness Probe configuration
	flags.StringVar(&opts.LivenessProbeOptions.Path, "liveness-probe-path", "",
		"Path to the file for the liveness probe.")
	flags.DurationVar(&opts.LivenessProbeOptions.UpdateInterval, "liveness-probe-interval", 0,
		"Interval of updating file for the liveness probe.")
	flags.DurationVar(&opts.probeCheckInterval, "probe-check-interval", defaultProbeCheckInterval,
		"Interval of checking the liveness of the CA.")

	flags.BoolVar(&opts.appendDNSNames, "append-dns-names", true,
		"Append DNS names to the certificates for webhook services.")

	rootCmd.AddCommand(version.CobraCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio CA",
		Section: "istio_ca CLI",
		Manual:  "Istio CA",
	}))

	rootCmd.AddCommand(cmd.NewProbeCmd())

	opts.loggingOptions.AttachCobraFlags(rootCmd)
	cmd.InitializeFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}

func runCA() {
	if err := log.Configure(opts.loggingOptions); err != nil {
		fatalf("Failed to configure logging (%v)", err)
	}

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
	}

	cs := createClientset()
	ca := createCA(cs.CoreV1())
	// For workloads in K8s, we apply the configured workload cert TTL.
	sc, err := controller.NewSecretController(ca, opts.workloadCertTTL, opts.workloadCertGracePeriodRatio,
		opts.workloadCertMinGracePeriod, cs.CoreV1(), opts.listenedNamespace, webhooks)
	if err != nil {
		fatalf("failed to create secret controller: %v", err)
	}

	stopCh := make(chan struct{})
	sc.Run(stopCh)

	if opts.grpcPort > 0 {
		// start registry if gRPC server is to be started
		reg := registry.GetIdentityRegistry()

		// add certificate identity to the identity registry for the liveness probe check
		if err := reg.AddMapping(probecontroller.LivenessProbeClientIdentity,
			probecontroller.LivenessProbeClientIdentity); err != nil {
			log.Errorf("failed to add indentity mapping: %v", err)
		}

		ch := make(chan struct{})

		// monitor service objects with "alpha.istio.io/kubernetes-serviceaccounts" annotation
		serviceController := kube.NewServiceController(cs.CoreV1(), opts.listenedNamespace, reg)
		serviceController.Run(ch)

		// monitor service account objects for istio mesh expansion
		serviceAccountController := kube.NewServiceAccountController(cs.CoreV1(), opts.listenedNamespace, reg)
		serviceAccountController.Run(ch)

		// The CA API uses cert with the max workload cert TTL.
		grpcServer := grpc.New(ca, opts.maxWorkloadCertTTL, opts.grpcHostname, opts.grpcPort)
		if err := grpcServer.Run(); err != nil {
			// stop the registry-related controllers
			ch <- struct{}{}

			log.Warnf("Failed to start GRPC server with error: %v", err)
		}
	}

	log.Info("Istio CA has started")
	select {} // wait forever
}

func createClientset() *kubernetes.Clientset {
	c := generateConfig()
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		fatalf("Failed to create a clientset (error: %s)", err)
	}
	return cs
}

func createCA(core corev1.SecretsGetter) ca.CertificateAuthority {
	var caOpts *ca.IstioCAOptions
	var err error

	if opts.upstreamCAAddress != "" {
		log.Info("Rely on upstream CA to provision CA certificate")
		caOpts, err = ca.NewIntegratedIstioCAOptions(opts.upstreamCAAddress, opts.upstreamCACertFile,
			opts.upstreamAuth, opts.workloadCertTTL, opts.maxWorkloadCertTTL)
		if err != nil {
			fatalf("Failed to create a integrated Istio CA (error: %v)", err)
		}
	} else if opts.selfSignedCA {
		log.Info("Use self-signed certificate as the CA certificate")
		caOpts, err = ca.NewSelfSignedIstioCAOptions(opts.selfSignedCACertTTL, opts.workloadCertTTL,
			opts.maxWorkloadCertTTL, opts.selfSignedCAOrg, opts.istioCaStorageNamespace, core)
		if err != nil {
			fatalf("Failed to create a self-signed Istio CA (error: %v)", err)
		}
	} else {
		log.Info("Use certificate from argument as the CA certificate")
		caOpts, err = ca.NewPluggedCertIstioCAOptions(opts.certChainFile, opts.signingCertFile, opts.signingKeyFile,
			opts.rootCertFile, opts.workloadCertTTL, opts.maxWorkloadCertTTL)
		if err != nil {
			fatalf("Failed to create an Istio CA (error: %v)", err)
		}
	}

	caOpts.LivenessProbeOptions = opts.LivenessProbeOptions
	caOpts.ProbeCheckInterval = opts.probeCheckInterval

	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		log.Errorf("Failed to create an Istio CA (error: %v)", err)
	}

	if opts.LivenessProbeOptions.IsValid() {
		var g interface{} = &caclient.CAGrpcClientImpl{}
		if client, ok := g.(caclient.CAGrpcClient); ok {
			livenessProbeChecker, err := probecontroller.NewLivenessCheckController(
				opts.probeCheckInterval, opts.grpcHostname, opts.grpcPort, istioCA,
				opts.LivenessProbeOptions, client)
			if err != nil {
				log.Errorf("failed to create an liveness probe check controller (error: %v)", err)
			} else {
				livenessProbeChecker.Run()
			}
		}
	}

	return istioCA
}

func generateConfig() *rest.Config {
	if opts.kubeConfigFile != "" {
		c, err := clientcmd.BuildConfigFromFlags("", opts.kubeConfigFile)
		if err != nil {
			fatalf("Failed to create a config object from file %s, (error %v)", opts.kubeConfigFile, err)
		}
		return c
	}

	// When `kubeConfigFile` is unspecified, use the in-cluster configuration.
	c, err := rest.InClusterConfig()
	if err != nil {
		fatalf("Failed to create a in-cluster config (error: %s)", err)
	}
	return c
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
