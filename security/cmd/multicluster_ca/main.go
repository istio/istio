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
	"io/ioutil"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/server/grpc"
)

const (
	defaultSigningCertTTL = 365 * 24 * time.Hour

	maxIssuedCertTTL = 15 * 24 * time.Hour

	// The default issuer organization for self-signed CA certificate.
	selfSignedCAOrgDefault = "k8s.cluster.local"

	// The key for the environment variable that specifies the namespace.
	namespaceKey = "NAMESPACE"
)

type cliOptions struct {
	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	istioCaStorageNamespace string

	kubeConfigFile string

	selfSignedCA    bool
	selfSignedCAOrg string

	signingCertTTL   time.Duration
	maxIssuedCertTTL time.Duration

	grpcHostname string
	grpcPort     int

	loggingOptions *log.Options
}

var (
	opts = cliOptions{
		loggingOptions: log.NewOptions(),
	}

	rootCmd = &cobra.Command{
		Use:   "multicluster_ca",
		Short: "Istio Multicluster Certificate Authority (CA)",
		Run: func(cmd *cobra.Command, args []string) {
			runCA()
		},
	}
)

func fatalf(template string, args ...interface{}) {
	log.Errorf(template, args)
	os.Exit(-1)
}

func init() {
	flags := rootCmd.Flags()

	flags.StringVar(&opts.certChainFile, "cert-chain", "", "Speicifies path to the certificate chain file")
	flags.StringVar(&opts.signingCertFile, "signing-cert", "", "Specifies path to the CA signing certificate file")
	flags.StringVar(&opts.signingKeyFile, "signing-key", "", "Specifies path to the CA signing key file")
	flags.StringVar(&opts.rootCertFile, "root-cert", "", "Specifies path to the root certificate file")

	flags.StringVar(&opts.istioCaStorageNamespace, "istio-ca-storage-namespace", "istio-system", "Namespace where "+
		"the Istio CA pods are running. Will not be used if explicit file or other storage mechanism is specified.")

	flags.StringVar(&opts.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	flags.BoolVar(&opts.selfSignedCA, "self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '--signing-cert' and '--signing-key' options are ignored.")
	flags.StringVar(&opts.selfSignedCAOrg, "self-signed-ca-org", "k8s.cluster.local",
		fmt.Sprintf("The issuer organization used in self-signed CA certificate (default to %s)",
			selfSignedCAOrgDefault))

	flags.DurationVar(&opts.signingCertTTL, "signing-cert-ttl", defaultSigningCertTTL,
		"The TTL of self-signed CA signing certificate")
	flags.DurationVar(&opts.maxIssuedCertTTL, "max-issued-cert-ttl", maxIssuedCertTTL, "The max TTL of issued certificates")

	flags.StringVar(&opts.grpcHostname, "grpc-hostname", "localhost", "Specifies the hostname for GRPC server.")
	flags.IntVar(&opts.grpcPort, "grpc-port", 0, "Specifies the port number for GRPC server. "+
		"If unspecified, Istio CA will not serve GRPC request.")

	rootCmd.AddCommand(version.CobraCommand())

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

	if value, exists := os.LookupEnv(namespaceKey); exists {
		// Use environment variable for istioCaStorageNamespace if it exists
		opts.istioCaStorageNamespace = value
	}

	verifyCommandLineOptions()

	cs := createClientset()
	ca := createCA(cs.CoreV1())

	// The CA API uses cert with the max workload cert TTL.
	grpcServer := grpc.New(ca, opts.maxIssuedCertTTL, opts.grpcHostname, opts.grpcPort)
	if err := grpcServer.Run(); err != nil {
		log.Warnf("Failed to start GRPC server with error: %v", err)
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

	if opts.selfSignedCA {
		log.Info("Use self-signed certificate as the CA certificate")

		caOpts, err = ca.NewSelfSignedIstioCAOptions(opts.signingCertTTL, 0, /* unused */
			opts.maxIssuedCertTTL, opts.selfSignedCAOrg, opts.istioCaStorageNamespace, core)
		if err != nil {
			fatalf("Failed to create a self-signed Istio Multicluster CA (error: %v)", err)
		}
	} else {
		var certChainBytes []byte
		if opts.certChainFile != "" {
			certChainBytes = readFile(opts.certChainFile)
		}
		caOpts = &ca.IstioCAOptions{
			CertChainBytes:   certChainBytes,
			CertTTL:          0, // unused
			MaxCertTTL:       opts.maxIssuedCertTTL,
			SigningCertBytes: readFile(opts.signingCertFile),
			SigningKeyBytes:  readFile(opts.signingKeyFile),
			RootCertBytes:    readFile(opts.rootCertFile),
		}
	}

	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		log.Errorf("Failed to create an Istio CA (error: %v)", err)
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

func readFile(filename string) []byte {
	bs, err := ioutil.ReadFile(filename)
	if err != nil {
		fatalf("Failed to read file %s (error: %v)", filename, err)
	}
	return bs
}

func verifyCommandLineOptions() {
	if opts.grpcHostname == "" {
		fatalf("GRPC host name is not specified.")
	}

	if opts.grpcPort <= 0 || opts.grpcPort > 65535 {
		fatalf("GPRC port number is invalid.")
	}

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
}
