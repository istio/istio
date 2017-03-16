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

	"istio.io/auth/certmanager"
	"istio.io/auth/controller"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	caCertFile     = flag.String("ca-cert", "", "Specifies path to the CA certificate file")
	caKeyFile      = flag.String("ca-key", "", "Specifies path to the CA key file")
	kubeConfigFile = flag.String("kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")
	selfSignedCA = flag.Bool("self-signed-ca", false,
		"Indicates whether to use auto-generated self-signed CA certificate. "+
			"When set to true, the '-ca-cert' and '-ca-key' options are ignored.")
)

func main() {
	flag.Parse()

	verifyCommandLineOptions()

	ca := createCA()
	cs := createClientset()
	sc := controller.NewSecretController(ca, cs.CoreV1())

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

		return certmanager.NewSelfSignedIstioCA()
	}

	c, k := certmanager.LoadSignerCredsFromFiles(*caCertFile, *caKeyFile)
	return certmanager.NewIstioCA(c, k)
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

func verifyCommandLineOptions() {
	if *selfSignedCA {
		return
	}

	if *caCertFile == "" {
		glog.Fatalf(
			"No CA cert has been specified. Either specify a cert file via '-ca-cert' option or use '-self-signed-ca'")
	}

	if *caKeyFile == "" {
		glog.Fatalf(
			"No CA key has been specified. Either specify a key file via '-ca-key' option or use '-self-signed-ca'")
	}
}
