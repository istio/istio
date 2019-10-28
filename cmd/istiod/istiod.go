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
	"os"

	"istio.io/pkg/log"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/istiod"
	"istio.io/istio/pkg/istiod/k8s"
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

	// First create the k8s clientset - and return the config source.
	// The config includes the address of apiserver and the public key - which will be used
	// after cert generation, to check that Apiserver-generated certs have same key.
	client, kcfg, err := k8s.CreateClientset(os.Getenv("KUBECONFIG"), "")
	if err != nil {
		// TODO: 'local' mode where k8s is not used - using the config.
		log.Fatalf("Failed to connect to k8s: %v", err)
	}

	// Load the mesh config. Note that the path is slightly changed - attempting to move all istio
	// related under /var/lib/istio, which is also the home dir of the istio user.
	istiods, err := istiod.NewIstiod(kcfg, client, "/var/lib/istio/config")
	if err != nil {
		log.Fatalf("Failed to start istiod: %v", err)
	}

	// Create k8s-signed certificates. This allows injector, validation to work without Citadel, and
	// allows secure SDS connections to Istiod.
	initCerts(istiods, client)

	// Init k8s related components, including Galley K8S controllers and
	// Pilot discovery. Code kept in separate package.
	k8sServer, err := k8s.InitK8S(istiods, client, kcfg, istiods.Args)
	if err != nil {
		log.Fatalf("Failed to start k8s controllers: %v", err)
	}

	err = istiods.InitDiscovery()
	if err != nil {
		log.Fatalf("Failed to init XDS server: %v", err)
	}

	if _, err := k8sServer.InitK8SDiscovery(istiods, kcfg, istiods.Args); err != nil {
		log.Fatalf("Failed to init Kubernetes discovery: %v", err)
	}

	err = istiods.Start(stop, k8sServer.OnXDSStart)
	if err != nil {
		log.Fatalf("Failed on start XDS server: %v", err)
	}

	// Injector should run along, even if not used - but only if the injection template is mounted.
	if _, err := os.Stat("./var/lib/istio/inject/injection-template.yaml"); err == nil {
		err = k8s.StartInjector(stop)
		if err != nil {
			log.Fatalf("Failure to start injector: %v", err)
		}
	}

	// Options based on the current 'defaults' in istio.
	// If adjustments are needed - env or mesh.config ( if of general interest ).

	istiod.RunCA(istiods.SecureGRPCServer, client, &istiod.CAOptions{
		TrustDomain: istiods.Mesh.TrustDomain,
	})

	istiods.Serve(stop)
	istiods.WaitStop(stop)
}

func initCerts(server *istiod.Server, client *kubernetes.Clientset) {
	// TODO: fallback to citadel (or custom CA)

	certChain, keyPEM, err := k8s.GenKeyCertK8sCA(client.CertificatesV1beta1(), istiod.IstiodNamespace.Get(),
		"istio-pilot."+istiod.IstiodNamespace.Get()+",istiod."+istiod.IstiodNamespace.Get())
	if err != nil {
		log.Fatal("Failed to initialize certs")
	}
	server.CertChain = certChain
	server.CertKey = keyPEM

	// Save the certificates to /var/run/secrets/istio-dns
	if err := os.MkdirAll(istiod.DNSCertDir, 0700); err != nil {
		log.Fatalf("Failed to create certs dir: %v", err)
	}
	err = ioutil.WriteFile(istiod.DNSCertDir+"/key.pem", keyPEM, 0700)
	if err != nil {
		log.Fatalf("Failed to write certs: %v", err)
	}
	err = ioutil.WriteFile(istiod.DNSCertDir+"/cert-chain.pem", certChain, 0700)
	if err != nil {
		log.Fatal("Failed to write certs")
	}
}
