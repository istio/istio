// Copyright Istio Authors
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

package istioagent

import (
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/security"

	"istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/nodeagent/cache"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"

	"istio.io/pkg/log"
)

// To debug:
// curl -X POST localhost:15000/logging?config=trace - to see SendingDiscoveryRequest

// Breakpoints in secretcache.go GenerateSecret..

// Note that istiod currently can't validate the JWT token unless it runs on k8s
// Main problem is the JWT validation check which hardcodes the k8s server address and token location.
//
// To test on a local machine, for debugging:
//
// kis exec $POD -- cat /run/secrets/istio-token/istio-token > var/run/secrets/tokens/istio-token
// kis port-forward $POD 15010:15010 &
//
// You can also copy the K8S CA and a token to be used to connect to k8s - but will need removing the hardcoded addr
// kis exec $POD -- cat /run/secrets/kubernetes.io/serviceaccount/{ca.crt,token} > var/run/secrets/kubernetes.io/serviceaccount/
//
// Or disable the jwt validation while debugging SDS problems.

var (
	// Location of K8S CA root.
	k8sCAPath = "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// CitadelCACertPath is the directory for Citadel CA certificate.
	// This is mounted from config map 'istio-ca-root-cert'. Part of startup,
	// this may be replaced with ./etc/certs, if a root-cert.pem is found, to
	// handle secrets mounted from non-citadel CAs.
	CitadelCACertPath = "./var/run/secrets/istio"
)

var (
	// LocalSDS is the location of the in-process SDS server - must be in a writeable dir.
	LocalSDS = "./etc/istio/proxy/SDS"

	gatewaySecretChan chan struct{}
)

// Agent contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type Agent struct {
	// SDSAddress is the address of the SDS server. Starts with unix: for hostpath mount or built-in
	// May also be a https address.
	SDSAddress string

	// CertPath is set with the location of the certs, or empty if mounted certs are not present.
	CertsPath string

	// RootCert is the CA root certificate. It is loaded part of detecting the
	// SDS operating mode - may be the Citadel CA, Kubernentes CA or a custom
	// CA. If not set it should be assumed we are using a public certificate (like ACME).
	RootCert []byte

	// WorkloadSecrets is the interface used to get secrets. The SDS agent
	// is calling this.
	WorkloadSecrets security.SecretManager

	// If set, this is the Citadel client, used to retrieve certificates.
	CitadelClient security.Client

	// Expected SAN for the discovery address, for tests.
	XDSSAN string

	proxyConfig *mesh.ProxyConfig

	// Listener for the XDS proxy
	LocalXDSListener net.Listener

	// ProxyGen is a generator for proxied types - will 'generate' XDS by using
	// an adsc connection.
	proxyGen *xds.ProxyGen

	// used for XDS portion.
	localListener   net.Listener
	localGrpcServer *grpc.Server

	xdsServer *xds.SimpleServer

	cfg     *AgentConfig
	secOpts *security.Options
}

// AgentConfig contains additional config for the agent, not included in ProxyConfig.
// Most are from env variables ( still experimental ) or for testing only.
// Eventually most non-test settings should graduate to ProxyConfig
// Please don't add 100 parameters to the NewAgent function (or any other)!
type AgentConfig struct {
	// LocalXDSAddr is the address of the XDS proxy. If not set, the env variable XDS_LOCAL will be used.
	// ( we may use ProxyConfig if this needs to be exposed, or we can base it on the base port - 15000)
	// Set for tests to 127.0.0.1:0.
	LocalXDSAddr string
}

// NewAgent wraps the logic for a local SDS. It will check if the JWT token required for local SDS is
// present, and set additional config options for the in-process SDS agent.
//
// The JWT token is currently using a pre-defined audience (istio-ca) or it must match the trust domain (WIP).
// If the JWT token is not present, and cannot be fetched through the credential fetcher - the local SDS agent can't authenticate.
//
// If node agent and JWT are mounted: it indicates user injected a config using hostPath, and will be used.
func NewAgent(proxyConfig *mesh.ProxyConfig, cfg *AgentConfig,
	sopts *security.Options) *Agent {
	sa := &Agent{
		proxyConfig: proxyConfig,
		cfg:         cfg,
		secOpts:     sopts,
	}

	// Fix the defaults - mainly for tests ( main uses env )
	if sopts.RecycleInterval.Seconds() == 0 {
		sopts.RecycleInterval = 5 * time.Minute
	}

	discAddr := proxyConfig.DiscoveryAddress

	sa.SDSAddress = "unix:" + LocalSDS

	// Auth logic for istio-agent to Cert provider:
	// - if PROV_CERT is set, it'll be included in the TLS context sent to the server
	//   This is a 'provisioning certificate' - long lived, managed by a tool, exchanged for
	//   the short lived certs.
	// - if a JWTPath token exists, or can be fetched by credential fetcher, it will be included in the request.

	// If original /etc/certs or a separate 'provisioning certs' (VM) are present,
	// add them to the tlsContext. If server asks for them and they exist - will be provided.
	certDir := "./etc/certs"
	if citadel.ProvCert != "" {
		certDir = citadel.ProvCert
	}
	if _, err := os.Stat(certDir + "/key.pem"); err == nil {
		sa.CertsPath = certDir
	}
	if sa.CertsPath != "" {
		log.Warna("Using existing certificate ", sa.CertsPath)
	}

	// If the root-cert is in the old location, use it.
	if _, err := os.Stat(certDir + "/root-cert.pem"); err == nil {
		CitadelCACertPath = certDir
	}

	if sa.secOpts.CAEndpoint == "" {
		// if not set, we will fallback to the discovery address
		sa.secOpts.CAEndpoint = discAddr
	}

	// Next to the envoy config, writeable dir (mounted as mem)
	sa.secOpts.WorkloadUDSPath = LocalSDS
	sa.secOpts.CertsDir = sa.CertsPath
	// Set TLSEnabled if the ControlPlaneAuthPolicy is set to MUTUAL_TLS
	if sa.proxyConfig.ControlPlaneAuthPolicy == mesh.AuthenticationPolicy_MUTUAL_TLS {
		sa.secOpts.TLSEnabled = true
	} else {
		sa.secOpts.TLSEnabled = false
	}
	// If proxy is using file mounted certs, JWT token is not needed.
	sa.secOpts.UseLocalJWT = !sa.secOpts.FileMountedCerts

	// Init the XDS proxy part of the agent.
	sa.initXDS()

	return sa
}

// Simplified SDS setup. This is called if and only if user has explicitly mounted a K8S JWT token, and is not
// using a hostPath mounted or external SDS server.
//
// 1. External CA: requires authenticating the trusted JWT AND validating the SAN against the JWT.
//    For example Google CA
//
// 2. Indirect, using istiod: using K8S cert.
//
// 3. Monitor mode - watching secret in same namespace ( Ingress)
//
// 4. TODO: File watching, for backward compat/migration from mounted secrets.
func (sa *Agent) Start(isSidecar bool, podNamespace string) (*sds.Server, error) {

	// TODO: remove the caching, workload has a single cert
	if sa.WorkloadSecrets == nil {
		sa.WorkloadSecrets, _ = sa.newWorkloadSecretCache()
	}

	var gatewaySecretCache *cache.SecretCache
	if !isSidecar {
		if gatewaySdsExists() {
			log.Infof("Starting gateway SDS")
			sa.secOpts.EnableGatewaySDS = true
			// TODO: what is the setting for ingress ?
			sa.secOpts.GatewayUDSPath = strings.TrimPrefix(model.GatewaySdsUdsPath, "unix:")
			gatewaySecretCache = sa.newSecretCache(podNamespace)
		} else {
			log.Infof("Skipping gateway SDS")
			sa.secOpts.EnableGatewaySDS = false
		}
	}

	server, err := sds.NewServer(sa.secOpts, sa.WorkloadSecrets, gatewaySecretCache)
	if err != nil {
		return nil, err
	}

	// Start the XDS client and proxy.
	err = sa.startXDS(sa.proxyConfig, sa.WorkloadSecrets)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func gatewaySdsExists() bool {
	p := strings.TrimPrefix(model.GatewaySdsUdsPath, "unix:")
	dir := path.Dir(p)
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

// newWorkloadSecretCache creates the cache for workload secrets and/or gateway secrets.
func (sa *Agent) newWorkloadSecretCache() (workloadSecretCache *cache.SecretCache, caClient security.Client) {
	fetcher := &secretfetcher.SecretFetcher{}

	// TODO: get the MC public keys from pilot.
	// In node agent, a controller is used getting 'istio-security.istio-system' config map
	// Single caTLSRootCert inside.

	var err error

	workloadSecretCache = cache.NewSecretCache(fetcher, sds.NotifyProxy, sa.secOpts)

	// If proxy is using file mounted certs, we do not have to connect to CA.
	if sa.secOpts.FileMountedCerts {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return
	}

	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	log.Infof("sa.serverOptions.CAEndpoint == %v", sa.secOpts.CAEndpoint)
	if sa.secOpts.CAProviderName == "GoogleCA" || strings.Contains(sa.secOpts.CAEndpoint, "googleapis.com") {
		// Use a plugin to an external CA - this has direct support for the K8S JWT token
		// This is only used if the proper env variables are injected - otherwise the existing Citadel or Istiod will be
		// used.
		caClient, err = gca.NewGoogleCAClient(sa.secOpts.CAEndpoint, true)
		sa.secOpts.PluginNames = []string{"GoogleTokenExchange"}
	} else {
		// Determine the default CA.
		// If /etc/certs exists - it means Citadel is used (possibly in a mode to only provision the root-cert, not keys)
		// Otherwise: default to istiod
		//
		// If an explicit CA is configured, assume it is mounting /etc/certs
		var rootCert []byte

		tls := true
		certReadErr := false

		if sa.secOpts.CAEndpoint == "" {
			// When sa.serverOptions.CAEndpoint is nil, the default CA endpoint
			// will be a hardcoded default value (e.g., the namespace will be hardcoded
			// as istio-system).
			log.Info("Istio Agent uses default istiod CA")
			sa.secOpts.CAEndpoint = "istiod.istio-system.svc:15012"

			if sa.secOpts.PilotCertProvider == "istiod" {
				log.Info("istiod uses self-issued certificate")
				if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
					certReadErr = true
				} else {
					log.Infof("the CA cert of istiod is: %v", string(rootCert))
				}
			} else if sa.secOpts.PilotCertProvider == "kubernetes" {
				log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
				if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
					certReadErr = true
				}
			} else if sa.secOpts.PilotCertProvider == "custom" {
				log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
					security.DefaultRootCertFilePath)
				if rootCert, err = ioutil.ReadFile(security.DefaultRootCertFilePath); err != nil {
					certReadErr = true
				}
			} else {
				certReadErr = true
			}
			if certReadErr {
				rootCert = nil
				// for debugging only
				log.Warnf("Failed to load root cert, assume IP secure network: %v", err)
				sa.secOpts.CAEndpoint = "istiod.istio-system.svc:15010"
				tls = false
			}
		} else {
			// Explicitly configured CA
			log.Infoa("Using user-configured CA ", sa.secOpts.CAEndpoint)
			if strings.HasSuffix(sa.secOpts.CAEndpoint, ":15010") {
				log.Warna("Debug mode or IP-secure network")
				tls = false
			} else if sa.secOpts.TLSEnabled {
				if sa.secOpts.PilotCertProvider == "istiod" {
					log.Info("istiod uses self-issued certificate")
					if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
						certReadErr = true
					} else {
						log.Infof("the CA cert of istiod is: %v", string(rootCert))
					}
				} else if sa.secOpts.PilotCertProvider == "kubernetes" {
					log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
					if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
						certReadErr = true
					}
				} else if sa.secOpts.PilotCertProvider == "custom" {
					log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
						security.DefaultRootCertFilePath)
					if rootCert, err = ioutil.ReadFile(security.DefaultRootCertFilePath); err != nil {
						certReadErr = true
					}
				} else {
					log.Errorf("unknown cert provider %v", sa.secOpts.PilotCertProvider)
					certReadErr = true
				}
				if certReadErr {
					rootCert = nil
					log.Fatal("invalid config - port 15012 missing a root certificate")
				}
			} else {
				rootCertPath := path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
				if rootCert, err = ioutil.ReadFile(rootCertPath); err != nil {
					// We may not provide root cert, and can just use public system certificate pool
					log.Infof("no certs found at %v, using system certs", rootCertPath)
				} else {
					log.Infof("the CA cert of istiod is: %v", string(rootCert))
				}
			}
		}

		// rootCert is used as a bundle - it can include multiple root certs !
		// If nil, the 'system' (public CA) roots are used to connect to the CA.
		sa.RootCert = rootCert

		// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
		// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
		// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
		caClient, err = citadel.NewCitadelClient(sa.secOpts.CAEndpoint, tls, rootCert, sa.secOpts.ClusterID)
		if err == nil {
			sa.CitadelClient = caClient
		}
	}

	// This has to be called after sa.secOpts.PluginNames is set. Otherwise,
	// TokenExchanger will contain an empty plugin, causing cert provisioning to fail.
	if sa.secOpts.TokenExchangers == nil {
		sa.secOpts.TokenExchangers = sds.NewPlugins(sa.secOpts.PluginNames)
	}

	if err != nil {
		log.Errorf("failed to create secretFetcher for workload proxy: %v", err)
		os.Exit(1)
	}
	fetcher.UseCaClient = true
	fetcher.CaClient = caClient

	return
}

// TODO: use existing 'sidecar/router' config to enable loading Secrets
func (sa *Agent) newSecretCache(namespace string) (gatewaySecretCache *cache.SecretCache) {
	gSecretFetcher := &secretfetcher.SecretFetcher{
		UseCaClient: false,
	}
	// TODO: use the common init !
	// If gateway is using file mounted certs, we do not have to setup secret fetcher.
	cs, err := kube.CreateClientset("", "")
	if err != nil {
		log.Errorf("failed to create secretFetcher for gateway proxy: %v", err)
		os.Exit(1)
	}

	gSecretFetcher.FallbackSecretName = "gateway-fallback"

	gSecretFetcher.InitWithKubeClientAndNs(cs.CoreV1(), namespace)

	gatewaySecretChan = make(chan struct{})
	gSecretFetcher.Run(gatewaySecretChan)
	gatewaySecretCache = cache.NewSecretCache(gSecretFetcher, sds.NotifyProxy, sa.secOpts)
	return gatewaySecretCache
}
