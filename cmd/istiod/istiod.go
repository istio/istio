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
	"io/ioutil"
	"istio.io/pkg/env"
	"log"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/istiod"
	"istio.io/istio/pkg/istiod/k8s"
)

var (
	// TODO: should it default to $HOME ? Or /var/lib/istio ?
	istioHome = env.RegisterStringVar("ISTIO_HOME", "",
		"Base directory for istio configs. If not set, defaults to current working directory.")
)

// Istio control plane with K8S support.
//
// - config is loaded from local K8S (in-cluster or using KUBECONFIG)
// - local endpoints and additional registries from K8S
// - additional MCP registries supported
// - includes a Secret controller that provisions certs as secrets.
//
// Normal hyperistio is using local config files and MCP sources for config/endpoints,
// as well as SDS backed by a file-based CA.
func main() {
	stop := make(chan struct{})

	// Load the mesh config. Note that the path is slightly changed - attempting to move all istio
	// related under /var/lib/istio, which is also the home dir of the istio user.
	istiods, err := istiod.NewIstiod("/var/lib/istio/config")
	if err != nil {
		log.Fatal("Failed to start istiod ", err)
	}

	// First create the k8s clientset - and return the config source.
	// The config includes the address of apiserver and the public key - which will be used
	// after cert generation, to check that Apiserver-generated certs have same key.
	client, kcfg, err := k8s.CreateClientset(os.Getenv("KUBECONFIG"), "")
	if err != nil {
		// TODO: 'local' mode where k8s is not used - using the config.
		log.Fatal("Failed to connect to k8s", err)
	}

	// Create k8s-signed certificates. This allows injector, validation to work without Citadel, and
	// allows secure SDS connections to Istiod.
	initCerts(istiods, client, kcfg)

	// Init k8s related components, including Galley K8S controllers and
	// Pilot discovery. Code kept in separate package.
	k8sServer, err := k8s.InitK8S(istiods, client, kcfg, istiods.Args)
	if err != nil {
		log.Fatal("Failed to start k8s controllers ", err)
	}

	// Initialize Galley config source for K8S.
	galleyK8S, err := k8sServer.NewGalleyK8SSource(istiods.Galley.Resources)
	istiods.Galley.Sources = append(istiods.Galley.Sources, galleyK8S)

	err = istiods.InitDiscovery()
	if err != nil {
		log.Fatal("Failed to init XDS server ", err)
	}

	k8sServer.InitK8SDiscovery(istiods, kcfg, istiods.Args)

	err = istiods.Start(stop, k8sServer.OnXDSStart)
	if err != nil {
		log.Fatal("Failure on start XDS server", err)
	}

	// Injector should run along, even if not used - but only if the injection template is mounted.
	if _, err := os.Stat("./var/lib/istio/inject/injection-template.yaml"); err == nil {
		err = k8s.StartInjector(stop)
		if err != nil {
			log.Fatalf("Failure to start injector ", err)
		}
	}

	istiods.Serve(stop)
	istiods.WaitStop(stop)
}

func initCerts(server *istiod.Server, client *kubernetes.Clientset, cfg *rest.Config) {
	// TODO: fallback to citadel (or custom CA)

	certChain, keyPEM, err := k8s.GenKeyCertK8sCA(client.CertificatesV1beta1(), "istio-system",
		"istio-pilot.istio-system,istiod.istio-system")
	if err != nil {
		log.Fatal("Failed to initialize certs")
	}
	server.CertChain = certChain
	server.CertKey = keyPEM

	// Save the certificates to /var/run/secrets/istio-dns
	os.MkdirAll(istiod.DNSCertDir, 0700)
	err = ioutil.WriteFile(istiod.DNSCertDir+"/key.pem", keyPEM, 0700)
	if err != nil {
		log.Fatal("Failed to write certs", err)
	}
	err = ioutil.WriteFile(istiod.DNSCertDir+"/cert-chain.pem", certChain, 0700)
	if err != nil {
		log.Fatal("Failed to write certs")
	}
}
