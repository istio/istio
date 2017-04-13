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
	"flag"
	"io/ioutil"
	"os"
	"time"

	"istio.io/auth/certmanager"
	"istio.io/auth/controller"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// The key for the environment variable that specifies the namespace.
const namespaceKey = "NAMESPACE"

var (
	certChainFile   = flag.String("cert-chain", "", "Speicifies path to the certificate chain file")
	signingCertFile = flag.String("signing-cert", "", "Specifies path to the CA signing certificate file")
	signingKeyFile  = flag.String("signing-key", "", "Specifies path to the CA signing key file")
	rootCertFile    = flag.String("root-cert", "", "Specifies path to the root certificate file")
	namespace       = flag.String("namespace", "",
		"Select a namespace for the CA to listen to. If unspecified, Istio CA tries to use the ${"+namespaceKey+"} "+
			"environment variable. If neither is set, Istio CA listens to all namespaces.")

	kubeConfigFile = flag.String("kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")

	selfSignedCA = flag.Bool("self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '-ca-cert' and '-ca-key' options are ignored.")

	caCertTTL = flag.Duration("ca-cert-ttl", 240*time.Hour,
		"The TTL of self-signed CA root certificate (default to 10 days)")

	certTTL = flag.Duration("cert-ttl", time.Hour, "The TTL of issued certificates (default to 1 hour)")
)

func main() {
	flag.Parse()

	if *namespace == "" {
		// When -namespace is not set, try to read the namespace from environment variable.
		if value, exists := os.LookupEnv(namespaceKey); exists {
			*namespace = value
		}
	}

	verifyCommandLineOptions()

	ca := createCA()
	cs := createClientset()
	sc := controller.NewSecretController(ca, cs.CoreV1(), *namespace)

	stopCh := make(chan struct{})
	sc.Run(stopCh)

	<-stopCh
	glog.Warning("Istio CA has stopped")
}

func createClientset() *kubernetes.Clientset {
	c := generateConfig()
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		glog.Fatalf("Failed to create a clientset (error: %s)", err)
	}
	return cs
}

func createCA() certmanager.CertificateAuthority {
	if *selfSignedCA {
		glog.Info("Use self-signed certificate as the CA certificate")

		ca, err := certmanager.NewSelfSignedIstioCA(*caCertTTL, *certTTL)
		if err != nil {
			glog.Fatalf("Failed to create a self-signed Istio CA (error: %v)", err)
		}
		return ca
	}

	opts := &certmanager.IstioCAOptions{
		CertChainBytes:   readFile(certChainFile),
		CertTTL:          *certTTL,
		SigningCertBytes: readFile(signingCertFile),
		SigningKeyBytes:  readFile(signingKeyFile),
		RootCertBytes:    readFile(rootCertFile),
	}
	ca, err := certmanager.NewIstioCA(opts)
	if err != nil {
		glog.Errorf("Failed to create an Istio CA (error %v)", err)
	}
	return ca
}

func generateConfig() *rest.Config {
	if *kubeConfigFile != "" {
		c, err := clientcmd.BuildConfigFromFlags("", *kubeConfigFile)
		if err != nil {
			glog.Fatalf("Failed to create a config object from file %s, (error %v)", *kubeConfigFile, err)
		}
		return c
	}

	// When `kubeConfigFile` is unspecified, use the in-cluster configuration.
	c, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to create a in-cluster config (error: %s)", err)
	}
	return c
}

func readFile(filename *string) []byte {
	bs, err := ioutil.ReadFile(*filename)
	if err != nil {
		glog.Fatalf("Failed to read file %s (error: %v)", *filename, err)
	}
	return bs
}

func verifyCommandLineOptions() {
	if *selfSignedCA {
		return
	}

	if *certChainFile == "" {
		glog.Fatalf(
			"No certificate chain has been specified. Either specify a cert chain file via '-cert-chain' option " +
				"or use '-self-signed-ca'")
	}

	if *signingCertFile == "" {
		glog.Fatalf(
			"No signing cert has been specified. Either specify a cert file via '-signing-cert' option " +
				"or use '-self-signed-ca'")
	}

	if *signingKeyFile == "" {
		glog.Fatalf(
			"No signing key has been specified. Either specify a key file via '-signing-key' option " +
				"or use '-self-signed-ca'")
	}

	if *rootCertFile == "" {
		glog.Fatalf(
			"No root cert has been specified. Either specify a root cert file via '-root-cert' option " +
				"or use '-self-signed-ca'")
	}
}
