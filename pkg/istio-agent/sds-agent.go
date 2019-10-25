package istio_agent

import (
	"context"
	"io/ioutil"
	"istio.io/istio/pkg/kube"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	"os"
	"strings"
	"time"

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
	caEndpointEnv = env.RegisterStringVar(caEndpoint, "localhost:15010", "").Get()

	pluginNamesEnv             = env.RegisterStringVar(pluginNames, "", "").Get()
	enableIngressGatewaySDSEnv = env.RegisterBoolVar(enableIngressGatewaySDS, false, "").Get()

	trustDomainEnv                     = env.RegisterStringVar(trustDomain, "", "").Get()
	secretTTLEnv                       = env.RegisterDurationVar(secretTTL, 24*time.Hour, "").Get()
	secretRefreshGraceDurationEnv      = env.RegisterDurationVar(SecretRefreshGraceDuration, 1*time.Hour, "").Get()
	secretRotationIntervalEnv          = env.RegisterDurationVar(SecretRotationInterval, 10*time.Minute, "").Get()
	staledConnectionRecycleIntervalEnv = env.RegisterDurationVar(staledConnectionRecycleInterval, 5*time.Minute, "").Get()
	initialBackoffEnv                  = env.RegisterIntVar(InitialBackoff, 10, "").Get()
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

	// The environmental variable name for grace duration that secret is re-generated
	// before it's expired time.
	// example value format like "10m"
	SecretRefreshGraceDuration = "SECRET_GRACE_DURATION"

	// The environmental variable name for key rotation job running interval.
	// example value format like "20m"
	SecretRotationInterval = "SECRET_JOB_RUN_INTERVAL"

	// The environmental variable name for staled connection recycle job running interval.
	// example value format like "5m"
	staledConnectionRecycleInterval = "STALED_CONNECTION_RECYCLE_RUN_INTERVAL"

	// The environmental variable name for the initial backoff in milliseconds.
	// example value format like "10"
	InitialBackoff = "INITIAL_BACKOFF_MSEC"
)

var (
	// JWTPath is the default location of a JWT token to be used to authenticate with XDS and CA servers.
	// If the file is missing, the agent will fallback to using mounted certificates if XDS address is secure.
	JWTPath = "/var/run/secrets/tokens/istio-token"

	// HostSDSUDS is the location where host-path mounted UDS is located in current installers
	HostSDSUDS = "/var/run/sds/uds_path"

	// LocalSDS is the location of the in-process SDS server - must be in a writeable dir.
	LocalSDS = "/etc/istio/proxy/SDS"

	workloadSdsCacheOptions cache.Options
	gatewaySdsCacheOptions  cache.Options
	serverOptions           sds.Options
	gatewaySecretChan       chan struct{}
)

// AgentConf contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type AgentConf struct {
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
}

// DetectSDS will attempt to find nodeagent SDS and token. If not found will attempt to find
// a location to start SDS.
//
// If node agent and JWT are mounted: it indicates user injected a config using hostPath, and will be used.
//
func DetectSDS(discAddr string, tlsRequired bool) *AgentConf {
	ac := &AgentConf{}
	if _, err := os.Stat(JWTPath); err == nil {
		ac.JWTPath = JWTPath
	}
	if _, err := os.Stat(HostSDSUDS); err == nil {
		ac.SDSAddress = HostSDSUDS
	}
	if _, err := os.Stat("/etc/certsx/key.pem"); err == nil {
		ac.CertsPath = "/etc/certs"
	}
	if tlsRequired {
		ac.RequireCerts = true
	}

	return ac
}

// Simplified SDS setup.
//
// 1. External CA: requires authenticating the trusted JWT AND validating the SAN against the JWT.
//    For example Google CA
//
// 2. Indirect, using istiod: using K8S cert.
//
// 3. Monitor mode - watching secret in same namespace ( Ingress)
//
// 4. TODO: File watching, for backward compat/migration from mounted secrets.
func StartSDS(conf *AgentConf, isSidecar bool) (*sds.Server, error) {
	applyEnvVars()

	gatewaySdsCacheOptions = workloadSdsCacheOptions

	// Next to the envoy config, writeable dir (mounted as mem)
	serverOptions.WorkloadUDSPath = LocalSDS

	// TODO: remove the caching, workload has a single cert
	workloadSecretCache, _ := newSecretCache(serverOptions)

	var gatewaySecretCache *cache.SecretCache
	if !isSidecar {
		serverOptions.EnableIngressGatewaySDS = true
		// TODO: what is the setting for ingress ?
		serverOptions.IngressGatewayUDSPath = serverOptions.WorkloadUDSPath + "_ROUTER"
		gatewaySecretCache, _ = newSecretCache(serverOptions)
	}

	// For sidecar and ingress we need to first get the certificates for the workload.
	// We'll also save them in files, for backward compat with servers generating files
	// TODO: use caClient.CSRSign() directly
	if conf.CertsPath == "" {
		tok, err := ioutil.ReadFile(conf.JWTPath)
		if err != nil && conf.RequireCerts {
			log.Fatala("Failed to read token", err)
		} else {
			si, err := workloadSecretCache.GenerateSecret(context.Background(), "bootstrap", "default",
				string(tok))
			if err != nil {
				if conf.RequireCerts {
					log.Fatala("Failed to get certificates", err)
				} else {
					log.Warna("Failed to get certificate from CA", err)
				}
			}
			if si != nil {
				// For debugging and backward compat - we may not need it long term
				ioutil.WriteFile("/etc/istio/proxy/key.pem", si.PrivateKey, 0700)
				ioutil.WriteFile("/etc/istio/proxy/cert-chain.pem", si.CertificateChain, 0700)
				//ioutil.WriteFile("/etc/istio/proxy/root-cert.pem", si.RootCert, 0700)
			}
			sir, err := workloadSecretCache.GenerateSecret(context.Background(), "bootstrap", "ROOTCA",
				string(tok))
			if err != nil {
				if conf.RequireCerts {
					log.Fatala("Failed to get certificates", err)
				} else {
					log.Warna("Failed to get certificate from CA", err)
				}
			}
			if sir != nil {
				// For debugging and backward compat - we may not need it long term
				ioutil.WriteFile("/etc/istio/proxy/root-cert.pem", sir.RootCert, 0700)
			}
		}
	}

	server, err := sds.NewServer(serverOptions, workloadSecretCache, gatewaySecretCache)
	if err != nil {
		return nil, err
	}

	return server, nil
}

// newSecretCache creates the cache for workload secrets and/or gateway secrets.
func newSecretCache(serverOptions sds.Options) (workloadSecretCache *cache.SecretCache, caClient caClientInterface.Client) {
	ret := &secretfetcher.SecretFetcher{}

	// TODO: get the MC public keys from pilot.
	// TODO: root cert for Istiod from the K8S file or local override
	// In node agent, a controller is used getting 'istio-security.istio-system' config map
	// Single caTLSRootCert inside.

	var err error

	if "GoogleCA" == serverOptions.CAProviderName || strings.Contains(serverOptions.CAEndpoint, "googleapis.com") {
		caClient, err = gca.NewGoogleCAClient(serverOptions.CAEndpoint, true)
		serverOptions.PluginNames = []string{"GoogleTokenExchange"}
	} else {
		caClient, err = citadel.NewCitadelClient(serverOptions.CAEndpoint, false, nil) // true, rootCert)
	}

	if err != nil {
		log.Errorf("failed to create secretFetcher for workload proxy: %v", err)
		os.Exit(1)
	}
	ret.UseCaClient = true
	ret.CaClient = caClient

	workloadSdsCacheOptions.TrustDomain = serverOptions.TrustDomain
	workloadSdsCacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
	workloadSecretCache = cache.NewSecretCache(ret, sds.NotifyProxy, workloadSdsCacheOptions)
	return
}

// TODO: use existing 'sidecar/router' config to enable loading Secrets
func newIngressSecretCache(serverOptions sds.Options) (gatewaySecretCache *cache.SecretCache) {
	gSecretFetcher := &secretfetcher.SecretFetcher{}

	gSecretFetcher.UseCaClient = false
	cs, err := kube.CreateClientset("", "")

	if err != nil {
		log.Errorf("failed to create secretFetcher for gateway proxy: %v", err)
		os.Exit(1)
	}
	gSecretFetcher.FallbackSecretName = "gateway-fallback"
	gSecretFetcher.InitWithKubeClient(cs.CoreV1())

	gatewaySecretChan = make(chan struct{})
	gSecretFetcher.Run(gatewaySecretChan)
	gatewaySecretCache = cache.NewSecretCache(gSecretFetcher, sds.NotifyProxy, gatewaySdsCacheOptions)
	return gatewaySecretCache
}

func applyEnvVars() {
	serverOptions.PluginNames = strings.Split(pluginNamesEnv, ",")

	serverOptions.EnableWorkloadSDS = true

	serverOptions.EnableIngressGatewaySDS = enableIngressGatewaySDSEnv
	serverOptions.CAProviderName = caProviderEnv
	serverOptions.CAEndpoint = caEndpointEnv
	serverOptions.TrustDomain = trustDomainEnv
	workloadSdsCacheOptions.SecretTTL = secretTTLEnv
	workloadSdsCacheOptions.SecretRefreshGraceDuration = secretRefreshGraceDurationEnv
	workloadSdsCacheOptions.RotationInterval = secretRotationIntervalEnv

	serverOptions.RecycleInterval = staledConnectionRecycleIntervalEnv

	workloadSdsCacheOptions.InitialBackoff = int64(initialBackoffEnv)
}
