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
	"istio.io/istio/pkg/istiod"
	"istio.io/istio/pkg/istiod/k8s"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"os"
	"time"

	meshv1 "istio.io/api/mesh/v1alpha1"
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
		log.Fatal("Failed to connect to k8s", err)
	}

	// Load the mesh config. Note that the path is slightly changed - attempting to move all istio
	// related under /var/lib/istio, which is also the home dir of the istio user.
	istiods, err := istiod.InitConfig("/var/lib/istio/config")
	if err != nil {
		log.Fatal("Failed to start istiod ", err)
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

	k8sServer.InitK8SDiscovery(istiods, client, kcfg, istiods.Args)

	// TODO: fixme, need to start the GRPC server first, wait for full sync.
	if false {
		k8sServer.WaitForCacheSync(stop)
	}

	// Off for now - working on replacement/simplified version
	// StartSDSK8S(baseDir, s.Mesh)

	err = istiods.Start(stop, k8sServer.OnXDSStart)
	if err != nil {
		log.Fatal("Failure on start XDS server", err)
	}

	// Feedback: don't run envoy sidecar in istiod
	//err = s.StartEnvoy(baseDir, kc.IstioServer.Mesh)
	//if err != nil {
	//	log.Fatal("Failure on start istio-control sidecar", err)
	//}

	// Injector should run along, even if not used - but only if the injection template is mounted.
	if _, err := os.Stat("./var/lib/istio/inject/injection-template.yaml"); err == nil {
		err = k8s.StartInjector(stop)
		if err != nil {
			log.Fatalf("Failure to start injector ", err)
		}
	}

	istiods.WaitDrain(".")
}

func initCerts(server *istiod.Server, client *kubernetes.Clientset, cfg *rest.Config) {
	// TODO: fallback to citadel (or custom CA)

	certChain, keyPEM, err := k8s.GenKeyCertK8sCA(client.CertificatesV1beta1(), "istio-system",
		"istio-pilot.istio-system")
	if err != nil {
		log.Fatal("Failed to initialize certs")
	}
	server.CertChain = certChain
	server.CertKey = keyPEM

	// Save the certificates to /var/run/secrets/istio-dns
	os.MkdirAll("./var/run/secrets/istio-dns", 0700)
	err = ioutil.WriteFile("./var/run/secrets/istio-dns/key.pem", keyPEM, 0700)
	if err != nil {
		log.Fatal("Failed to write certs", err)
	}
	err = ioutil.WriteFile("./var/run/secrets/istio-dns/cert-chain.pem", certChain, 0700)
	if err != nil {
		log.Fatal("Failed to write certs")
	}
}

// Start the workload SDS server. Will run on the UDS path - Envoy sidecar will use a cluster
// to expose the UDS path over TLS, using Apiserver-signed certs.
// SDS depends on k8s.
//
// TODO: modify NewSecretFetcher, add method taking a kube client ( for consistency )
// TODO: modify NewServer, add method taking a grpcServer
func StartSDSK8S(baseDir string, config *meshv1.MeshConfig) error {

	// This won't work on VM - only on K8S.
	var sdsCacheOptions cache.Options
	var serverOptions sds.Options

	// Compat with Istio env - will determine the plugin used for connecting to the CA.
	caProvider := os.Getenv("CA_PROVIDER")
	if caProvider == "" {
		caProvider = "Citadel"
	}

	// Compat with Istio env
	// Will use istio-system/istio-security config map to load the root CA of citadel.
	// The address can be the Gateway address for the cluster
	// TODO: load the GW address of the cluster to auto-configure
	// TODO: Citadel should also use k8s-api signed certificates ( possibly on different port )
	caAddr := os.Getenv("CA_ADDR")
	if caAddr == "" {
		// caAddr = "istio-citadel.istio-system:8060"
		// For testing with port fwd (kfwd istio-system istio=citadel 8060:8060)
		caAddr = "localhost:8060"
	}

	wSecretFetcher, err := secretfetcher.NewSecretFetcher(false,
		caAddr, caProvider, true,
		[]byte(""), "", "", "", "")
	if err != nil {
		log.Fatal("failed to create secretFetcher for workload proxy", err)
	}
	sdsCacheOptions.TrustDomain = serverOptions.TrustDomain
	sdsCacheOptions.RotationInterval = 5 * time.Minute
	serverOptions.RecycleInterval = 5 * time.Minute
	serverOptions.EnableWorkloadSDS = true
	serverOptions.WorkloadUDSPath = "./sdsUDS"
	sdsCacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
	workloadSecretCache := cache.NewSecretCache(wSecretFetcher, sds.NotifyProxy, sdsCacheOptions)

	// GatewaySecretCache loads secrets from K8S - they should be in same namespace with ingress gateway, will use
	// standalone node agent.
	_, err = sds.NewServer(serverOptions, workloadSecretCache, nil)

	if err != nil {
		log.Fatal("Failed to start SDS server", err)
	}

	return nil
}
