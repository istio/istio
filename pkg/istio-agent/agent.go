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
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/dns"
	"istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/cache"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
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
)

const (
	MetadataClientCertKey   = "ISTIO_META_TLS_CLIENT_KEY"
	MetadataClientCertChain = "ISTIO_META_TLS_CLIENT_CERT_CHAIN"
	MetadataClientRootCert  = "ISTIO_META_TLS_CLIENT_ROOT_CERT"
)

// Agent contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type Agent struct {
	// RootCert is the CA root certificate. It is loaded part of detecting the
	// SDS operating mode - may be the Citadel CA, Kubernentes CA or a custom
	// CA. If not set it should be assumed we are using a public certificate (like ACME).
	RootCert []byte

	// WorkloadSecrets is the interface used to get secrets. The SDS agent
	// is calling this.
	WorkloadSecrets security.SecretManager

	// If set, this is the Citadel upstream, used to retrieve certificates.
	CitadelClient security.Client

	proxyConfig *mesh.ProxyConfig

	cfg     *AgentConfig
	secOpts *security.Options

	// used for local XDS generator portion, to download all configs and generate xds locally.
	localXDSGenerator *localXDSGenerator

	// Used when proxying envoy xds via istio-agent is enabled.
	xdsProxy *XdsProxy

	// local DNS Server that processes DNS requests locally and forwards to upstream DNS if needed.
	localDNSServer *dns.LocalDNSServer
}

// AgentConfig contains additional config for the agent, not included in ProxyConfig.
// Most are from env variables ( still experimental ) or for testing only.
// Eventually most non-test settings should graduate to ProxyConfig
// Please don't add 100 parameters to the NewAgent function (or any other)!
type AgentConfig struct {
	// ProxyXDSViaAgent if true will enable a local XDS proxy that will simply
	// ferry Envoy's XDS requests to istiod and responses back to envoy
	// This flag is temporary until the feature is stabilized.
	ProxyXDSViaAgent bool
	// DNSCapture indicates if the XDS proxy has dns capture enabled or not
	// This option will not be considered if proxyXDSViaAgent is false.
	DNSCapture bool
	// ProxyNamespace to use for local dns resolution
	ProxyNamespace string
	// ProxyDomain is the DNS domain associated with the proxy (assumed
	// to include the namespace as well) (for local dns resolution)
	ProxyDomain string

	// LocalXDSGeneratorListenAddress is the address where the agent will listen for XDS connections and generate all
	// xds configurations locally. If not set, the env variable LOCAL_XDS_GENERATOR will be used.
	// Set for tests to 127.0.0.1:0.
	LocalXDSGeneratorListenAddress string

	// Grpc dial options. Used for testing
	GrpcOptions []grpc.DialOption

	// XDSRootCerts is the location of the root CA for the XDS connection. Used for setting platform certs or
	// using custom roots.
	XDSRootCerts string

	// CARootCerts of the location of the root CA for the CA connection. Used for setting platform certs or
	// using custom roots.
	CARootCerts string
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
	// If the root-cert is in the old location, use it.
	if _, err := os.Stat(certDir + "/root-cert.pem"); err == nil {
		log.Warna("Using existing certificate ", certDir)
		CitadelCACertPath = certDir
	}

	if sa.secOpts.CAEndpoint == "" {
		// if not set, we will fallback to the discovery address
		sa.secOpts.CAEndpoint = discAddr
	}
	// Next to the envoy config, writeable dir (mounted as mem)
	if sa.secOpts.WorkloadUDSPath == "" {
		sa.secOpts.WorkloadUDSPath = LocalSDS
	}
	// Set TLSEnabled if the ControlPlaneAuthPolicy is set to MUTUAL_TLS
	if sa.proxyConfig.ControlPlaneAuthPolicy == mesh.AuthenticationPolicy_MUTUAL_TLS {
		sa.secOpts.TLSEnabled = true
	} else {
		sa.secOpts.TLSEnabled = false
	}
	// If proxy is using file mounted certs, JWT token is not needed.
	sa.secOpts.UseLocalJWT = !sa.secOpts.FileMountedCerts

	// Init the XDS proxy part of the agent.
	sa.initXDSGenerator()

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
			sa.secOpts.GatewayUDSPath = strings.TrimPrefix(model.CredentialNameSDSUdsPath, "unix:")
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

	// Start the local XDS generator.
	if sa.localXDSGenerator != nil {
		err = sa.startXDSGenerator(sa.proxyConfig, sa.WorkloadSecrets, podNamespace)
		if err != nil {
			return nil, fmt.Errorf("failed to start local xds generator: %v", err)
		}
	}

	if err = sa.initLocalDNSServer(isSidecar); err != nil {
		return nil, fmt.Errorf("failed to start local DNS server: %v", err)
	}
	if sa.cfg.ProxyXDSViaAgent {
		sa.xdsProxy, err = initXdsProxy(sa)
		if err != nil {
			return nil, fmt.Errorf("failed to start xds proxy: %v", err)
		}
	}
	return server, nil
}

func (sa *Agent) initLocalDNSServer(isSidecar bool) (err error) {
	// we dont need dns server on gateways
	if sa.cfg.DNSCapture && sa.cfg.ProxyXDSViaAgent && isSidecar {
		if sa.localDNSServer, err = dns.NewLocalDNSServer(sa.cfg.ProxyNamespace, sa.cfg.ProxyDomain); err != nil {
			return err
		}
		sa.localDNSServer.StartDNS()
	}
	return nil
}

func (sa *Agent) Close() {
	if sa.xdsProxy != nil {
		sa.xdsProxy.close()
	}
	if sa.localDNSServer != nil {
		sa.localDNSServer.Close()
	}
	sa.closeLocalXDSGenerator()
}

func (sa *Agent) GetLocalXDSGeneratorListener() net.Listener {
	if sa.localXDSGenerator != nil {
		return sa.localXDSGenerator.listener
	}
	return nil
}

func gatewaySdsExists() bool {
	p := strings.TrimPrefix(model.CredentialNameSDSUdsPath, "unix:")
	dir := path.Dir(p)
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

// explicit code to determine the root CA to be configured in bootstrap file.
// It may be different from the CA for the cert server - which is based on CA_ADDR
// Replaces logic in the template:
//                 {{- if .provisioned_cert }}
//                  "filename": "{{(printf "%s%s" .provisioned_cert "/root-cert.pem") }}"
//                  {{- else if eq .pilot_cert_provider "kubernetes" }}
//                  "filename": "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
//                  {{- else if eq .pilot_cert_provider "istiod" }}
//                  "filename": "./var/run/secrets/istio/root-cert.pem"
//                  {{- end }}
//
// In addition it deals with the case the XDS server is on port 443, expected with a proper cert.
// /etc/ssl/certs/ca-certificates.crt
//
// TODO: additional checks for existence. Fail early, instead of obscure envoy errors.
func (sa *Agent) FindRootCAForXDS() string {
	if sa.cfg.XDSRootCerts != "" {
		return sa.cfg.XDSRootCerts
	} else if _, err := os.Stat("./etc/certs/root-cert.pem"); err == nil {
		// Old style - mounted cert. This is used for XDS auth only,
		// not connecting to CA_ADDR because this mode uses external
		// agent (Secret refresh, etc)
		return "./etc/certs/root-cert.pem"
	} else if sa.secOpts.PilotCertProvider == "kubernetes" {
		// Using K8S - this is likely incorrect, may work by accident.
		// API is alpha.
		return k8sCAPath
	} else if sa.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		return sa.secOpts.ProvCert + "/root-cert.pem"
	} else if sa.secOpts.FileMountedCerts {
		// FileMountedCerts - Load it from Proxy Metadata.
		return sa.proxyConfig.ProxyMetadata[MetadataClientRootCert]
	} else {
		// PILOT_CERT_PROVIDER - default is istiod
		// This is the default - a mounted config map on K8S
		return path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
	}
}

// Find the root CA to use when connecting to the CA (Istiod or external).
//
func (sa *Agent) FindRootCAForCA() string {
	if sa.cfg.CARootCerts != "" {
		return sa.cfg.CARootCerts
	} else if sa.secOpts.PilotCertProvider == "kubernetes" {
		// Using K8S - this is likely incorrect, may work by accident.
		// API is alpha.
		return k8sCAPath // ./var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	} else if sa.secOpts.PilotCertProvider == "custom" {
		return security.DefaultRootCertFilePath // ./etc/certs/root-cert.pem
	} else if sa.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		return sa.secOpts.ProvCert + "/root-cert.pem"
	} else {
		// This is the default - a mounted config map on K8S
		return path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
		// or: "./var/run/secrets/istio/root-cert.pem"
	}
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
	// FILE_MOUNTED_CERTS=true
	if sa.secOpts.FileMountedCerts {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return
	}

	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	log.Infof("sa.serverOptions.CAEndpoint == %v %s", sa.secOpts.CAEndpoint, sa.secOpts.CAProviderName)
	if sa.secOpts.CAProviderName == "GoogleCA" || strings.Contains(sa.secOpts.CAEndpoint, "googleapis.com") {
		// Use a plugin to an external CA - this has direct support for the K8S JWT token
		// This is only used if the proper env variables are injected - otherwise the existing Citadel or Istiod will be
		// used.
		caClient, err = gca.NewGoogleCAClient(sa.secOpts.CAEndpoint, true)
		sa.secOpts.PluginNames = []string{"GoogleTokenExchange"}
		sa.secOpts.TokenExchangers = []security.TokenExchanger{stsclient.NewPlugin()}
	} else {
		var rootCert []byte
		// Special case: if Istiod runs on a secure network, on the default port, don't use TLS
		// TODO: may add extra cases or explicit settings - but this is a rare use cases, mostly debugging
		tls := true
		if strings.HasSuffix(sa.secOpts.CAEndpoint, ":15010") {
			tls = false
			log.Warna("Debug mode or IP-secure network")
		}
		if tls {
			caCertFile := sa.FindRootCAForCA()
			if rootCert, err = ioutil.ReadFile(caCertFile); err != nil {
				log.Fatalf("invalid config - %s missing a root certificate %s", sa.secOpts.CAEndpoint, caCertFile)
			} else {
				log.Infof("Using CA %s cert with certs: %s", sa.secOpts.CAEndpoint, caCertFile)

				sa.RootCert = rootCert
			}
		}

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
	fetcher.CaClient = caClient

	return
}

// TODO: use existing 'sidecar/router' config to enable loading Secrets
func (sa *Agent) newSecretCache(namespace string) (gatewaySecretCache *cache.SecretCache) {
	gSecretFetcher := &secretfetcher.SecretFetcher{}
	// TODO: use the common init !
	// If gateway is using file mounted certs, we do not have to setup secret fetcher.
	if !sa.secOpts.FileMountedCerts {
		cs, err := kube.CreateClientset("", "")
		if err != nil {
			log.Errorf("failed to create secretFetcher for gateway proxy: %v", err)
			os.Exit(1)
		}

		gSecretFetcher.FallbackSecretName = "gateway-fallback"

		gSecretFetcher.InitWithKubeClientAndNs(cs.CoreV1(), namespace)

		stopCh := make(chan struct{})
		gSecretFetcher.Run(stopCh)
	}

	gatewaySecretCache = cache.NewSecretCache(gSecretFetcher, sds.NotifyProxy, sa.secOpts)
	return gatewaySecretCache
}
