package istio_agent

import (
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
	caProviderEnv                      = env.RegisterStringVar(caProvider, "Citadel", "").Get()
	// TODO: default to same as discovery address
	caEndpointEnv                      = env.RegisterStringVar(caEndpoint, "localhost:15010", "").Get()

	pluginNamesEnv                     = env.RegisterStringVar(pluginNames, "", "").Get()
	enableIngressGatewaySDSEnv         = env.RegisterBoolVar(enableIngressGatewaySDS, false, "").Get()

	trustDomainEnv                     = env.RegisterStringVar(trustDomain, "", "").Get()
	secretTTLEnv                       = env.RegisterDurationVar(secretTTL, 24*time.Hour, "").Get()
	secretRefreshGraceDurationEnv      = env.RegisterDurationVar(SecretRefreshGraceDuration, 1*time.Hour, "").Get()
	secretRotationIntervalEnv          = env.RegisterDurationVar(SecretRotationInterval, 10*time.Minute, "").Get()
	staledConnectionRecycleIntervalEnv = env.RegisterDurationVar(staledConnectionRecycleInterval, 5*time.Minute, "").Get()
	initialBackoffEnv                  = env.RegisterIntVar(InitialBackoff, 10, "").Get()
)

const (
	// name of authentication provider.
	caProvider     = "CA_PROVIDER"

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
	workloadSdsCacheOptions cache.Options
	gatewaySdsCacheOptions  cache.Options
	serverOptions           sds.Options
	gatewaySecretChan       chan struct{}
)

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
func StartSDS() (*sds.Server, error) {
	applyEnvVars()
	gatewaySdsCacheOptions = workloadSdsCacheOptions

	// Next to the envoy config, writeable dir (mounted as mem)
	serverOptions.WorkloadUDSPath = "./var/lib/istio/proxy/SDS"
	// TODO: remove the caching, workload has a single cert
	workloadSecretCache := newSecretCache(serverOptions)

	var gatewaySecretCache *cache.SecretCache
	if serverOptions.EnableIngressGatewaySDS {
		 gatewaySecretCache = newSecretCache(serverOptions)
	}

	server, err := sds.NewServer(serverOptions, workloadSecretCache, gatewaySecretCache)
	if err != nil {
		return nil, err
	}

	return server, nil
}

// newSecretCache creates the cache for workload secrets and/or gateway secrets.
func newSecretCache(serverOptions sds.Options) (workloadSecretCache *cache.SecretCache) {
	ret := &secretfetcher.SecretFetcher{}

	// TODO: get the MC public keys from pilot.
	// TODO: root cert for Istiod from the K8S file or local override
	// In node agent, a controller is used getting 'istio-security.istio-system' config map
	// Single caTLSRootCert inside.

	var err error
	var caClient caClientInterface.Client
	if "GoogleCA" == serverOptions.CAProviderName {
		caClient, err = gca.NewGoogleCAClient(serverOptions.CAEndpoint, true)
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

		return workloadSecretCache
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
