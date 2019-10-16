package main

import (
	"flag"
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
	flag.Parse()
	stop := make(chan struct{})

	// Where to load mesh config - assumes on K8S we run with workdir=="/", and while testing
	// or on VMs it runs in a specific directory, keeping with the relative paths ( etc/certs, etc)
	// This allows maximum compatibility and minimize disruption while still supporting non-root use.

	client, kcfg, err := k8s.CreateClientset(os.Getenv("KUBECONFIG"), "")
	if err != nil {
		log.Fatal("Failed to connect to k8s", err)
	}

	s, err := istiod.InitConfig("/var/lib/istio/config")
	if err != nil {
		log.Fatal("Failed to start ", err)
	}

	// InitConfig certificates - first thing.
	initCerts(s, client, kcfg)

	kc, err := k8s.InitK8S(s, client, kcfg, s.Args)
	if err != nil {
		log.Fatal("Failed to start k8s", err)
	}

	// Initialize Galley config source for K8S.
	galleyK8S, err := kc.NewGalleyK8SSource(s.Galley.Resources)
	s.Galley.Sources = append(s.Galley.Sources, galleyK8S)

	err = s.InitDiscovery()
	if err != nil {
		log.Fatal("Failed to start ", err)
	}

	kc.InitK8SDiscovery(s, client, kcfg, s.Args)

	if false {
		kc.WaitForCacheSync(stop)
	}

	// Off for now - working on replacement/simplified version
	// StartSDSK8S(baseDir, s.Mesh)

	err = s.Start(stop, kc.OnXDSStart)
	if err != nil {
		log.Fatal("Failure on start", err)
	}

	// Feedback: don't run envoy sidecar in istiod
	//err = s.StartEnvoy(baseDir, kc.IstioServer.Mesh)
	//if err != nil {
	//	log.Fatal("Failure on start istio-control sidecar", err)
	//}

	// Injector should run along, even if not used.
	err = k8s.StartInjector(stop)
	if err != nil {
		//log.Fatal("Failure on start injector", err)
		log.Println("Failure to start injector - ignore for now ", err)
	}

	s.WaitDrain(".")
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
