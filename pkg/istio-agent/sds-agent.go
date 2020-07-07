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

package istioagent

import (
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"istio.io/istio/pkg/config/constants"

	"istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/kube"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	"istio.io/pkg/env"
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
	caProviderEnv = env.RegisterStringVar(caProvider, "Citadel", "").Get()
	// TODO: default to same as discovery address
	caEndpointEnv = env.RegisterStringVar(caEndpoint, "", "").Get()

	pluginNamesEnv             = env.RegisterStringVar(pluginNames, "", "").Get()
	enableIngressGatewaySDSEnv = env.RegisterBoolVar(enableIngressGatewaySDS, false, "").Get()

	trustDomainEnv = env.RegisterStringVar(trustDomain, "", "").Get()
	secretTTLEnv   = env.RegisterDurationVar(secretTTL, 24*time.Hour,
		"The cert lifetime requested by istio agent").Get()
	secretRotationGracePeriodRatioEnv = env.RegisterFloatVar(secretRotationGracePeriodRatio, 0.5,
		"The grace period ratio for the cert rotation, by default 0.5.").Get()
	secretRotationIntervalEnv = env.RegisterDurationVar(secretRotationInterval, 5*time.Minute,
		"The ticker to detect and rotate the certificates, by default 5 minutes").Get()
	staledConnectionRecycleIntervalEnv = env.RegisterDurationVar(staledConnectionRecycleInterval, 5*time.Minute,
		"The ticker to detect and close stale connections").Get()
	initialBackoffInMilliSecEnv = env.RegisterIntVar(initialBackoffInMilliSec, 0, "").Get()
	pkcs8KeysEnv                = env.RegisterBoolVar(pkcs8Key, false, "Whether to generate PKCS#8 private keys").Get()
	eccSigAlgEnv                = env.RegisterStringVar(eccSigAlg, "", "The type of ECC signature algorithm to use when generating private keys").Get()

	// Location of K8S CA root.
	k8sCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// CitadelCACertPath is the directory for Citadel CA certificate.
	// This is mounted from config map 'istio-ca-root-cert'. Part of startup,
	// this may be replaced with ./etc/certs, if a root-cert.pem is found, to
	// handle secrets mounted from non-citadel CAs.
	CitadelCACertPath = "./var/run/secrets/istio"

	fileMountedCertsEnv = env.RegisterBoolVar(fileMountedCerts, false, "").Get()
)

const (
	// name of authentication provider.
	caProvider = "CA_PROVIDER"

	// CA endpoint.
	caEndpoint = "CA_ADDR"

	// names of authentication provider's plugins.
	pluginNames = "PLUGINS"

	// The trust domain corresponds to the trust root of a system.
	// Refer to https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	trustDomain = "TRUST_DOMAIN"

	// The ingress gateway SDS mode allows node agent to provision credentials to ingress gateway
	// proxy by watching kubernetes secrets.
	enableIngressGatewaySDS = "ENABLE_INGRESS_GATEWAY_SDS"

	// The environmental variable name for secret TTL, node agent decides whether a secret
	// is expired if time.now - secret.createtime >= secretTTL.
	// example value format like "90m"
	secretTTL = "SECRET_TTL"

	// The environmental variable name for grace period ratio that secret is re-generated before
	// its expiration.
	// Example value format like "0.5"
	secretRotationGracePeriodRatio = "SECRET_GRACE_PERIOD_RATIO"

	// The environmental variable name for the interval to check and rotate the key and certs.
	// Example value format like "20m"
	secretRotationInterval = "SECRET_ROTATION_CHECK_INTERVAL"

	// The environmental variable name for staled connection recycle job running interval.
	// example value format like "5m"
	staledConnectionRecycleInterval = "STALED_CONNECTION_RECYCLE_RUN_INTERVAL"

	// The environmental variable name for the initial backoff in milliseconds.
	// example value format like "10"
	initialBackoffInMilliSec = "INITIAL_BACKOFF_MSEC"

	pkcs8Key = "PKCS8_KEY"

	// The type of Elliptical Signature algorithm to use
	// when generating private keys. Currently only ECDSA is supported.
	eccSigAlg = "ECC_SIGNATURE_ALGORITHM"

	// Indicates whether proxy uses file mounted certificates.
	fileMountedCerts = "FILE_MOUNTED_CERTS"
)

var (
	// LocalSDS is the location of the in-process SDS server - must be in a writeable dir.
	LocalSDS = "./etc/istio/proxy/SDS"

	workloadSdsCacheOptions cache.Options
	gatewaySdsCacheOptions  cache.Options
	serverOptions           sds.Options
	gatewaySecretChan       chan struct{}
)

// SDSAgent contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type SDSAgent struct {
	// Location of JWTPath to connect to CA. If empty, SDS is not possible.
	// If set SDS will be used - either local or via hostPath.
	JWTPath string

	// SDSAddress is the address of the SDS server. Starts with unix: for hostpath mount or built-in
	// May also be a https address.
	SDSAddress string

	// CertPath is set with the location of the certs, or empty if mounted certs are not present.
	CertsPath string

	// RequireCerts is set if the agent requires certificates:
	// - if controlPlaneAuthEnabled is set
	// - port of discovery server is not 15010 (the plain text default).
	RequireCerts bool

	// PilotCertProvider is the provider of the Pilot certificate
	PilotCertProvider string

	// OutputKeyCertToDir is the directory for output the key and certificate
	OutputKeyCertToDir string

	// RootCert is the CA root certificate. It is loaded part of detecting the
	// SDS operating mode - may be the Citadel CA, Kubernentes CA or a custom
	// CA. If not set it should be assumed we are using a public certificate (like ACME).
	RootCert []byte

	// WorkloadSecrets is the interface used to get secrets. The SDS agent
	// is calling this.
	WorkloadSecrets *cache.SecretCache

	// If set, this is the Citadel client, used to retrieve certificates.
	CitadelClient caClientInterface.Client

	// CAEndpoint is the CA endpoint to which node agent sends CSR request.
	CAEndpoint string

	// ClusterID is the cluster where the agent resides
	ClusterID string

	// FileMountedCerts indicates whether the proxy is using file mounted certs.
	FileMountedCerts bool
}

// NewSDSAgent wraps the logic for a local SDS. It will check if the JWT token required for local SDS is
// present, and set additional config options for the in-process SDS agent.
//
// The JWT token is currently using a pre-defined audience (istio-ca) or it must match the trust domain (WIP).
// If the JWT token is not present - the local SDS agent can't authenticate.
//
// If node agent and JWT are mounted: it indicates user injected a config using hostPath, and will be used.
//
func NewSDSAgent(discAddr string, tlsRequired bool, pilotCertProvider, jwtPath, outputKeyCertToDir, clusterID string) *SDSAgent {
	a := &SDSAgent{}

	a.SDSAddress = "unix:" + LocalSDS
	a.ClusterID = clusterID

	// If a workload is using file mounted certs, we do not to have to process CA relaated configuration.
	if !shouldProvisionCertificates() {
		log.Info("Workload is using file mounted certificates. Skipping setting CA related configuration")
		a.FileMountedCerts = true
		return a
	}

	a.PilotCertProvider = pilotCertProvider
	a.OutputKeyCertToDir = outputKeyCertToDir

	_, discPort, err := net.SplitHostPort(discAddr)
	if err != nil {
		log.Fatalf("Invalid discovery address %v %v", discAddr, err)
	}

	if _, err := os.Stat(jwtPath); err == nil && citadel.ProvCert == "" {
		// If the JWT file exists, and explicit 'prov cert' is not - use the JWT
		a.JWTPath = jwtPath
	} else {
		// If original /etc/certs or a separate 'provisioning certs' (VM) are present, use them instead of tokens
		certDir := "./etc/certs"
		if citadel.ProvCert != "" {
			certDir = citadel.ProvCert
		}
		if _, err := os.Stat(certDir + "/key.pem"); err == nil {
			a.CertsPath = certDir
		}
		// If the root-cert is in the old location, use it.
		if _, err := os.Stat(certDir + "/root-cert.pem"); err == nil {
			CitadelCACertPath = certDir
		}

		if a.CertsPath != "" {
			log.Warna("Using existing certificate ", a.CertsPath)
		} else {
			// Can't use in-process SDS.
			log.Warna("Missing JWT token, can't use in process SDS ", jwtPath, err)

			// TODO do not special case port 15012
			if discPort == "15012" {
				log.Fatala("Missing JWT, can't authenticate with control plane. Try using plain text (15010)")
			}
			// continue to initialize the agent.
		}
	}

	a.CAEndpoint = caEndpointEnv
	if caEndpointEnv == "" {
		// if not set, we will fallback to the discovery address
		a.CAEndpoint = discAddr
	}

	if tlsRequired {
		a.RequireCerts = true
	}

	// Istiod uses a fixed, defined port for K8S-signed certificates.
	// TODO do not special case port 15012
	if discPort == "15012" {
		a.RequireCerts = true
	}

	return a
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
func (sa *SDSAgent) Start(isSidecar bool, podNamespace string) (*sds.Server, error) {
	applyEnvVars()

	gatewaySdsCacheOptions = workloadSdsCacheOptions

	serverOptions.PilotCertProvider = sa.PilotCertProvider
	// Next to the envoy config, writeable dir (mounted as mem)
	serverOptions.WorkloadUDSPath = LocalSDS
	serverOptions.CertsDir = sa.CertsPath
	serverOptions.JWTPath = sa.JWTPath
	serverOptions.OutputKeyCertToDir = sa.OutputKeyCertToDir
	serverOptions.CAEndpoint = sa.CAEndpoint
	serverOptions.TLSEnabled = sa.RequireCerts
	serverOptions.ClusterID = sa.ClusterID
	serverOptions.FileMountedCerts = sa.FileMountedCerts
	// If proxy is using file mounted certs, JWT token is not needed.
	if sa.FileMountedCerts {
		serverOptions.UseLocalJWT = false
	} else {
		serverOptions.UseLocalJWT = sa.CertsPath == "" // true if we don't have a key.pem
	}

	// TODO: remove the caching, workload has a single cert
	workloadSecretCache, _ := sa.newSecretCache(serverOptions)

	var gatewaySecretCache *cache.SecretCache
	if !isSidecar {
		if ingressSdsExists() {
			log.Infof("Starting gateway SDS")
			serverOptions.EnableIngressGatewaySDS = true
			// TODO: what is the setting for ingress ?
			serverOptions.IngressGatewayUDSPath = strings.TrimPrefix(model.IngressGatewaySdsUdsPath, "unix:")
			gatewaySecretCache = newIngressSecretCache(podNamespace)
		} else {
			log.Infof("Skipping gateway SDS")
		}
	}

	server, err := sds.NewServer(serverOptions, workloadSecretCache, gatewaySecretCache)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func ingressSdsExists() bool {
	p := strings.TrimPrefix(model.IngressGatewaySdsUdsPath, "unix:")
	dir := path.Dir(p)
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

// newSecretCache creates the cache for workload secrets and/or gateway secrets.
func (sa *SDSAgent) newSecretCache(serverOptions sds.Options) (workloadSecretCache *cache.SecretCache, caClient caClientInterface.Client) {
	fetcher := &secretfetcher.SecretFetcher{}

	// TODO: get the MC public keys from pilot.
	// In node agent, a controller is used getting 'istio-security.istio-system' config map
	// Single caTLSRootCert inside.

	var err error

	workloadSdsCacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
	workloadSecretCache = cache.NewSecretCache(fetcher, sds.NotifyProxy, workloadSdsCacheOptions)
	sa.WorkloadSecrets = workloadSecretCache

	// If proxy is using file mounted certs, we do not have to connect to CA.
	if !shouldProvisionCertificates() {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return
	}

	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	log.Infof("serverOptions.CAEndpoint == %v", serverOptions.CAEndpoint)
	if (serverOptions.CAProviderName == "GoogleCA" || strings.Contains(serverOptions.CAEndpoint, "googleapis.com")) &&
		stsclient.GKEClusterURL != "" {
		// Use a plugin to an external CA - this has direct support for the K8S JWT token
		// This is only used if the proper env variables are injected - otherwise the existing Citadel or Istiod will be
		// used.
		caClient, err = gca.NewGoogleCAClient(serverOptions.CAEndpoint, true)
		serverOptions.PluginNames = []string{"GoogleTokenExchange"}
	} else {
		// Determine the default CA.
		// If /etc/certs exists - it means Citadel is used (possibly in a mode to only provision the root-cert, not keys)
		// Otherwise: default to istiod
		//
		// If an explicit CA is configured, assume it is mounting /etc/certs
		var rootCert []byte

		tls := true
		certReadErr := false

		if serverOptions.CAEndpoint == "" {
			// When serverOptions.CAEndpoint is nil, the default CA endpoint
			// will be a hardcoded default value (e.g., the namespace will be hardcoded
			// as istio-system).
			log.Info("Istio Agent uses default istiod CA")
			serverOptions.CAEndpoint = "istiod.istio-system.svc:15012"

			if serverOptions.PilotCertProvider == "istiod" {
				log.Info("istiod uses self-issued certificate")
				if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
					certReadErr = true
				} else {
					log.Infof("the CA cert of istiod is: %v", string(rootCert))
				}
			} else if serverOptions.PilotCertProvider == "kubernetes" {
				log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
				if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
					certReadErr = true
				}
			} else if serverOptions.PilotCertProvider == "custom" {
				log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
					cache.DefaultRootCertFilePath)
				if rootCert, err = ioutil.ReadFile(cache.DefaultRootCertFilePath); err != nil {
					certReadErr = true
				}
			} else {
				certReadErr = true
			}
			if certReadErr {
				rootCert = nil
				// for debugging only
				log.Warnf("Failed to load root cert, assume IP secure network: %v", err)
				serverOptions.CAEndpoint = "istiod.istio-system.svc:15010"
				tls = false
			}
		} else {
			// Explicitly configured CA
			log.Infoa("Using user-configured CA ", serverOptions.CAEndpoint)
			if strings.HasSuffix(serverOptions.CAEndpoint, ":15010") {
				log.Warna("Debug mode or IP-secure network")
				tls = false
			} else if serverOptions.TLSEnabled {
				if serverOptions.PilotCertProvider == "istiod" {
					log.Info("istiod uses self-issued certificate")
					if rootCert, err = ioutil.ReadFile(path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)); err != nil {
						certReadErr = true
					} else {
						log.Infof("the CA cert of istiod is: %v", string(rootCert))
					}
				} else if serverOptions.PilotCertProvider == "kubernetes" {
					log.Infof("istiod uses the k8s root certificate %v", k8sCAPath)
					if rootCert, err = ioutil.ReadFile(k8sCAPath); err != nil {
						certReadErr = true
					}
				} else if serverOptions.PilotCertProvider == "custom" {
					log.Infof("istiod uses a custom root certificate mounted in a well known location %v",
						cache.DefaultRootCertFilePath)
					if rootCert, err = ioutil.ReadFile(cache.DefaultRootCertFilePath); err != nil {
						certReadErr = true
					}
				} else {
					log.Errorf("unknown cert provider %v", serverOptions.PilotCertProvider)
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

		sa.RootCert = rootCert
		// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
		// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
		// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
		caClient, err = citadel.NewCitadelClient(serverOptions.CAEndpoint, tls, rootCert, serverOptions.ClusterID)
		if err == nil {
			sa.CitadelClient = caClient
		}
	}

	if err != nil {
		log.Errorf("failed to create secretFetcher for workload proxy: %v", err)
		os.Exit(1)
	}
	fetcher.UseCaClient = true
	fetcher.CaClient = caClient

	workloadSdsCacheOptions.TrustDomain = serverOptions.TrustDomain
	workloadSdsCacheOptions.Pkcs8Keys = serverOptions.Pkcs8Keys
	workloadSdsCacheOptions.OutputKeyCertToDir = serverOptions.OutputKeyCertToDir

	return
}

// TODO: use existing 'sidecar/router' config to enable loading Secrets
func newIngressSecretCache(namespace string) (gatewaySecretCache *cache.SecretCache) {
	gSecretFetcher := &secretfetcher.SecretFetcher{
		UseCaClient: false,
	}

	// If gateway is using file mounted certs, we do not have to setup secret fetcher.
	if shouldProvisionCertificates() {
		cs, err := kube.CreateClientset("", "")

		if err != nil {
			log.Errorf("failed to create secretFetcher for gateway proxy: %v", err)
			os.Exit(1)
		}
		gSecretFetcher.FallbackSecretName = "gateway-fallback"

		gSecretFetcher.InitWithKubeClientAndNs(cs.CoreV1(), namespace)

		gatewaySecretChan = make(chan struct{})
		gSecretFetcher.Run(gatewaySecretChan)
	}
	gatewaySecretCache = cache.NewSecretCache(gSecretFetcher, sds.NotifyProxy, gatewaySdsCacheOptions)
	return gatewaySecretCache
}

func applyEnvVars() {
	serverOptions.PluginNames = strings.Split(pluginNamesEnv, ",")

	serverOptions.EnableWorkloadSDS = true

	serverOptions.EnableIngressGatewaySDS = enableIngressGatewaySDSEnv
	serverOptions.CAProviderName = caProviderEnv
	serverOptions.TrustDomain = trustDomainEnv
	serverOptions.Pkcs8Keys = pkcs8KeysEnv
	serverOptions.ECCSigAlg = eccSigAlgEnv
	serverOptions.RecycleInterval = staledConnectionRecycleIntervalEnv
	workloadSdsCacheOptions.ECCSigAlg = eccSigAlgEnv
	workloadSdsCacheOptions.SecretTTL = secretTTLEnv
	workloadSdsCacheOptions.SecretRotationGracePeriodRatio = secretRotationGracePeriodRatioEnv
	workloadSdsCacheOptions.RotationInterval = secretRotationIntervalEnv
	workloadSdsCacheOptions.InitialBackoffInMilliSec = int64(initialBackoffInMilliSecEnv)
	// Disable the secret eviction for istio agent.
	workloadSdsCacheOptions.EvictionDuration = 0
	if citadel.ProvCert != "" {
		workloadSdsCacheOptions.AlwaysValidTokenFlag = true
	}
	workloadSdsCacheOptions.OutputKeyCertToDir = serverOptions.OutputKeyCertToDir
}

// shouldProvisionCertificates returns true if certs needs to be provisioned for the workload/gateway.
// Returns flase, when proxy uses file mounted certificates i.e. FILE_MOUNTED_CERTS is true.
func shouldProvisionCertificates() bool {
	return !fileMountedCertsEnv
}
