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

package features

import (
	"strings"
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var (
	MaxConcurrentStreams = env.Register(
		"ISTIO_GPRC_MAXSTREAMS",
		100000,
		"Sets the maximum number of concurrent grpc streams.",
	).Get()

	// MaxRecvMsgSize The max receive buffer size of gRPC received channel of Pilot in bytes.
	MaxRecvMsgSize = env.Register(
		"ISTIO_GPRC_MAXRECVMSGSIZE",
		4*1024*1024,
		"Sets the max receive buffer size of gRPC stream in bytes.",
	).Get()

	traceSamplingVar = env.Register(
		"PILOT_TRACE_SAMPLING",
		1.0,
		"Sets the mesh-wide trace sampling percentage. Should be 0.0 - 100.0. Precision to 0.01. "+
			"Default is 1.0.",
	)

	TraceSampling = func() float64 {
		f := traceSamplingVar.Get()
		if f < 0.0 || f > 100.0 {
			log.Warnf("PILOT_TRACE_SAMPLING out of range: %v", f)
			return 1.0
		}
		return f
	}()

	PushThrottle = env.Register(
		"PILOT_PUSH_THROTTLE",
		100,
		"Limits the number of concurrent pushes allowed. On larger machines this can be increased for faster pushes",
	).Get()

	RequestLimit = env.Register(
		"PILOT_MAX_REQUESTS_PER_SECOND",
		25.0,
		"Limits the number of incoming XDS requests per second. On larger machines this can be increased to handle more proxies concurrently.",
	).Get()

	// FilterGatewayClusterConfig controls if a subset of clusters(only those required) should be pushed to gateways
	FilterGatewayClusterConfig = env.Register("PILOT_FILTER_GATEWAY_CLUSTER_CONFIG", false,
		"If enabled, Pilot will send only clusters that referenced in gateway virtual services attached to gateway").Get()

	DebounceAfter = env.Register(
		"PILOT_DEBOUNCE_AFTER",
		100*time.Millisecond,
		"The delay added to config/registry events for debouncing. This will delay the push by "+
			"at least this interval. If no change is detected within this period, the push will happen, "+
			" otherwise we'll keep delaying until things settle, up to a max of PILOT_DEBOUNCE_MAX.",
	).Get()

	DebounceMax = env.Register(
		"PILOT_DEBOUNCE_MAX",
		10*time.Second,
		"The maximum amount of time to wait for events while debouncing. If events keep showing up with no breaks "+
			"for this time, we'll trigger a push.",
	).Get()

	EnableEDSDebounce = env.Register(
		"PILOT_ENABLE_EDS_DEBOUNCE",
		true,
		"If enabled, Pilot will include EDS pushes in the push debouncing, configured by PILOT_DEBOUNCE_AFTER and PILOT_DEBOUNCE_MAX."+
			" EDS pushes may be delayed, but there will be fewer pushes. By default this is enabled",
	).Get()

	SendUnhealthyEndpoints = atomic.NewBool(env.Register(
		"PILOT_SEND_UNHEALTHY_ENDPOINTS",
		false,
		"If enabled, Pilot will include unhealthy endpoints in EDS pushes and even if they are sent Envoy does not use them for load balancing."+
			"  To avoid, sending traffic to non ready endpoints, enabling this flag, disables panic threshold in Envoy i.e. Envoy does not load balance requests"+
			" to unhealthy/non-ready hosts even if the percentage of healthy hosts fall below minimum health percentage(panic threshold).",
	).Get())

	EnablePersistentSessionFilter = env.Register(
		"PILOT_ENABLE_PERSISTENT_SESSION_FILTER",
		false,
		"If enabled, Istiod sets up persistent session filter for listeners, if services have 'PILOT_PERSISTENT_SESSION_LABEL' set.",
	).Get()

	PersistentSessionLabel = env.Register(
		"PILOT_PERSISTENT_SESSION_LABEL",
		"istio.io/persistent-session",
		"If not empty, services with this label will use cookie based persistent sessions",
	).Get()

	PersistentSessionHeaderLabel = env.Register(
		"PILOT_PERSISTENT_SESSION_HEADER_LABEL",
		"istio.io/persistent-session-header",
		"If not empty, services with this label will use header based persistent sessions",
	).Get()

	DrainingLabel = env.Register(
		"PILOT_DRAINING_LABEL",
		"istio.io/draining",
		"If not empty, endpoints with the label value present will be sent with status DRAINING.",
	).Get()

	// HTTP10 will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	HTTP10 = env.Register(
		"PILOT_HTTP10",
		false,
		"Enables the use of HTTP 1.0 in the outbound HTTP listeners, to support legacy applications.",
	).Get()

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = env.Register(
		"PILOT_ENABLE_MYSQL_FILTER",
		false,
		"EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.",
	).Get()

	// EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `redis`.
	EnableRedisFilter = env.Register(
		"PILOT_ENABLE_REDIS_FILTER",
		false,
		"EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.",
	).Get()

	// EnableMongoFilter enables injection of `envoy.filters.network.mongo_proxy` in the filter chain.
	EnableMongoFilter = env.Register(
		"PILOT_ENABLE_MONGO_FILTER",
		true,
		"EnableMongoFilter enables injection of `envoy.filters.network.mongo_proxy` in the filter chain.",
	).Get()

	// UseRemoteAddress sets useRemoteAddress to true for sidecar outbound listeners so that it picks up the localhost
	// address of the sender, which is an internal address, so that trusted headers are not sanitized.
	UseRemoteAddress = env.Register(
		"PILOT_SIDECAR_USE_REMOTE_ADDRESS",
		false,
		"UseRemoteAddress sets useRemoteAddress to true for sidecar outbound listeners.",
	).Get()

	// SkipValidateTrustDomain tells the server proxy to not to check the peer's trust domain when
	// mTLS is enabled in authentication policy.
	SkipValidateTrustDomain = env.Register(
		"PILOT_SKIP_VALIDATE_TRUST_DOMAIN",
		false,
		"Skip validating the peer is from the same trust domain when mTLS is enabled in authentication policy").Get()

	ScopeGatewayToNamespace = env.Register(
		"PILOT_SCOPE_GATEWAY_TO_NAMESPACE",
		false,
		"If enabled, a gateway workload can only select gateway resources in the same namespace. "+
			"Gateways with same selectors in different namespaces will not be applicable.",
	).Get()

	EnableHeadlessService = env.Register(
		"PILOT_ENABLE_HEADLESS_SERVICE_POD_LISTENERS",
		true,
		"If enabled, for a headless service/stateful set in Kubernetes, pilot will generate an "+
			"outbound listener for each pod in a headless service. This feature should be disabled "+
			"if headless services have a large number of pods.",
	).Get()

	JwksFetchMode = func() jwt.JwksFetchMode {
		v := env.Register(
			"PILOT_JWT_ENABLE_REMOTE_JWKS",
			"false",
			"Mode of fetching JWKs from JwksUri in RequestAuthentication. Supported value: "+
				"istiod, false, hybrid, true, envoy. The client fetching JWKs is as following: "+
				"istiod/false - Istiod; hybrid/true - Envoy and fallback to Istiod if JWKs server is external; "+
				"envoy - Envoy.",
		).Get()
		return jwt.ConvertToJwksFetchMode(v)
	}()

	EnableEDSForHeadless = env.Register(
		"PILOT_ENABLE_EDS_FOR_HEADLESS_SERVICES",
		false,
		"If enabled, for headless service in Kubernetes, pilot will send endpoints over EDS, "+
			"allowing the sidecar to load balance among pods in the headless service. This feature "+
			"should be enabled if applications access all services explicitly via a HTTP proxy port in the sidecar.",
	).Get()

	EnableDistributionTracking = env.Register(
		"PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING",
		false,
		"If enabled, Pilot will assign meaningful nonces to each Envoy configuration message, and allow "+
			"users to interrogate which envoy has which config from the debug interface.",
	).Get()

	DistributionHistoryRetention = env.Register(
		"PILOT_DISTRIBUTION_HISTORY_RETENTION",
		time.Minute*1,
		"If enabled, Pilot will keep track of old versions of distributed config for this duration.",
	).Get()

	MCSAPIGroup = env.Register("MCS_API_GROUP", "multicluster.x-k8s.io",
		"The group to be used for the Kubernetes Multi-Cluster Services (MCS) API.").Get()

	MCSAPIVersion = env.Register("MCS_API_VERSION", "v1alpha1",
		"The version to be used for the Kubernets Multi-Cluster Services (MCS) API.").Get()

	EnableMCSAutoExport = env.Register(
		"ENABLE_MCS_AUTO_EXPORT",
		false,
		"If enabled, istiod will automatically generate Kubernetes "+
			"Multi-Cluster Services (MCS) ServiceExport resources for every "+
			"service in the mesh. Services defined to be cluster-local in "+
			"MeshConfig are excluded.",
	).Get()

	EnableMCSServiceDiscovery = env.Register(
		"ENABLE_MCS_SERVICE_DISCOVERY",
		false,
		"If enabled, istiod will enable Kubernetes Multi-Cluster "+
			"Services (MCS) service discovery mode. In this mode, service "+
			"endpoints in a cluster will only be discoverable within the "+
			"same cluster unless explicitly exported via ServiceExport.").Get()

	EnableMCSHost = env.Register(
		"ENABLE_MCS_HOST",
		false,
		"If enabled, istiod will configure a Kubernetes Multi-Cluster "+
			"Services (MCS) host (<svc>.<namespace>.svc.clusterset.local) "+
			"for each service exported (via ServiceExport) in at least one "+
			"cluster. Clients must, however, be able to successfully lookup "+
			"these DNS hosts. That means that either Istio DNS interception "+
			"must be enabled or an MCS controller must be used. Requires "+
			"that ENABLE_MCS_SERVICE_DISCOVERY also be enabled.").Get() &&
		EnableMCSServiceDiscovery

	EnableMCSClusterLocal = env.Register(
		"ENABLE_MCS_CLUSTER_LOCAL",
		false,
		"If enabled, istiod will treat the host "+
			"`<svc>.<namespace>.svc.cluster.local` as defined by the "+
			"Kubernetes Multi-Cluster Services (MCS) spec. In this mode, "+
			"requests to `cluster.local` will be routed to only those "+
			"endpoints residing within the same cluster as the client. "+
			"Requires that both ENABLE_MCS_SERVICE_DISCOVERY and "+
			"ENABLE_MCS_HOST also be enabled.").Get() &&
		EnableMCSHost

	EnableAnalysis = env.Register(
		"PILOT_ENABLE_ANALYSIS",
		false,
		"If enabled, pilot will run istio analyzers and write analysis errors to the Status field of any "+
			"Istio Resources",
	).Get()

	AnalysisInterval = func() time.Duration {
		val, _ := env.Register(
			"PILOT_ANALYSIS_INTERVAL",
			10*time.Second,
			"If analysis is enabled, pilot will run istio analyzers using this value as interval in seconds "+
				"Istio Resources",
		).Lookup()
		if val < 1*time.Second {
			log.Warnf("PILOT_ANALYSIS_INTERVAL %s is too small, it will be set to default 10 seconds", val.String())
			return 10 * time.Second
		}
		return val
	}()

	EnableStatus = env.Register(
		"PILOT_ENABLE_STATUS",
		false,
		"If enabled, pilot will update the CRD Status field of all istio resources with reconciliation status.",
	).Get()

	StatusUpdateInterval = env.Register(
		"PILOT_STATUS_UPDATE_INTERVAL",
		500*time.Millisecond,
		"Interval to update the XDS distribution status.",
	).Get()

	StatusQPS = env.Register(
		"PILOT_STATUS_QPS",
		100,
		"If status is enabled, controls the QPS with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config QPS",
	).Get()

	StatusBurst = env.Register(
		"PILOT_STATUS_BURST",
		500,
		"If status is enabled, controls the Burst rate with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config Burst",
	).Get()

	StatusMaxWorkers = env.Register("PILOT_STATUS_MAX_WORKERS", 100, "The maximum number of workers"+
		" Pilot will use to keep configuration status up to date.  Smaller numbers will result in higher status latency, "+
		"but larger numbers may impact CPU in high scale environments.").Get()

	// IstiodServiceCustomHost allow user to bring a custom address or multiple custom addresses for istiod server
	// for examples: 1. istiod.mycompany.com  2. istiod.mycompany.com,istiod-canary.mycompany.com
	IstiodServiceCustomHost = env.Register("ISTIOD_CUSTOM_HOST", "",
		"Custom host name of istiod that istiod signs the server cert. "+
			"Multiple custom host names are supported, and multiple values are separated by commas.").Get()

	PilotCertProvider = env.Register("PILOT_CERT_PROVIDER", constants.CertProviderIstiod,
		"The provider of Pilot DNS certificate.").Get()

	JwtPolicy = env.Register("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.").Get()

	EnableGatewayAPI = env.Register("PILOT_ENABLE_GATEWAY_API", true,
		"If this is set to true, support for Kubernetes gateway-api (github.com/kubernetes-sigs/gateway-api) will "+
			" be enabled. In addition to this being enabled, the gateway-api CRDs need to be installed.").Get()

	EnableAlphaGatewayAPI = env.Register("PILOT_ENABLE_ALPHA_GATEWAY_API", false,
		"If this is set to true, support for alpha APIs in the Kubernetes gateway-api (github.com/kubernetes-sigs/gateway-api) will "+
			" be enabled. In addition to this being enabled, the gateway-api CRDs need to be installed.").Get()

	EnableGatewayAPIStatus = env.Register("PILOT_ENABLE_GATEWAY_API_STATUS", true,
		"If this is set to true, gateway-api resources will have status written to them").Get()

	EnableGatewayAPIDeploymentController = env.Register("PILOT_ENABLE_GATEWAY_API_DEPLOYMENT_CONTROLLER", true,
		"If this is set to true, gateway-api resources will automatically provision in cluster deployment, services, etc").Get()

	ClusterName = env.Register("CLUSTER_ID", "Kubernetes",
		"Defines the cluster and service registry that this Istiod instance belongs to").Get()

	ExternalIstiod = env.Register("EXTERNAL_ISTIOD", false,
		"If this is set to true, one Istiod will control remote clusters including CA.").Get()

	EnableCAServer = env.Register("ENABLE_CA_SERVER", true,
		"If this is set to false, will not create CA server in istiod.").Get()

	EnableDebugOnHTTP = env.Register("ENABLE_DEBUG_ON_HTTP", true,
		"If this is set to false, the debug interface will not be enabled, recommended for production").Get()

	MutexProfileFraction = env.Register("MUTEX_PROFILE_FRACTION", 1000,
		"If set to a non-zero value, enables mutex profiling a rate of 1/MUTEX_PROFILE_FRACTION events."+
			" For example, '1000' will record 0.1% of events. "+
			"Set to 0 to disable entirely.").Get()

	EnableUnsafeAdminEndpoints = env.Register("UNSAFE_ENABLE_ADMIN_ENDPOINTS", false,
		"If this is set to true, dangerous admin endpoints will be exposed on the debug interface. Not recommended for production.").Get()

	XDSAuth = env.Register("XDS_AUTH", true,
		"If true, will authenticate XDS clients.").Get()

	EnableXDSIdentityCheck = env.Register(
		"PILOT_ENABLE_XDS_IDENTITY_CHECK",
		true,
		"If enabled, pilot will authorize XDS clients, to ensure they are acting only as namespaces they have permissions for.",
	).Get()

	// TODO: Move this to proper API.
	trustedGatewayCIDR = env.Register(
		"TRUSTED_GATEWAY_CIDR",
		"",
		"If set, any connections from gateway to Istiod with this CIDR range are treated as trusted for using authentication mechanisms like XFCC."+
			" This can only be used when the network where Istiod and the authenticating gateways are running in a trusted/secure network",
	)

	TrustedGatewayCIDR = func() []string {
		cidr := trustedGatewayCIDR.Get()

		// splitting the empty string will result [""]
		if cidr == "" {
			return []string{}
		}

		return strings.Split(cidr, ",")
	}()

	CATrustedNodeAccounts = func() sets.Set[types.NamespacedName] {
		accounts := env.Register(
			"CA_TRUSTED_NODE_ACCOUNTS",
			"",
			"If set, the list of service accounts that are allowed to use node authentication for CSRs. "+
				"Node authentication allows an identity to create CSRs on behalf of other identities, but only if there is a pod "+
				"running on the same node with that identity. "+
				"This is intended for use with node proxies.",
		).Get()
		res := sets.New[types.NamespacedName]()
		if accounts == "" {
			return res
		}
		for _, v := range strings.Split(accounts, ",") {
			ns, sa, valid := strings.Cut(v, "/")
			if !valid {
				log.Warnf("Invalid CA_TRUSTED_NODE_ACCOUNTS, ignoring: %v", v)
				continue
			}
			res.Insert(types.NamespacedName{
				Namespace: ns,
				Name:      sa,
			})
		}
		return res
	}()

	EnableServiceEntrySelectPods = env.Register("PILOT_ENABLE_SERVICEENTRY_SELECT_PODS", true,
		"If enabled, service entries with selectors will select pods from the cluster. "+
			"It is safe to disable it if you are quite sure you don't need this feature").Get()

	EnableK8SServiceSelectWorkloadEntries = env.RegisterBoolVar("PILOT_ENABLE_K8S_SELECT_WORKLOAD_ENTRIES", true,
		"If enabled, Kubernetes services with selectors will select workload entries with matching labels. "+
			"It is safe to disable it if you are quite sure you don't need this feature").Get()

	InjectionWebhookConfigName = env.Register("INJECTION_WEBHOOK_CONFIG_NAME", "istio-sidecar-injector",
		"Name of the mutatingwebhookconfiguration to patch, if istioctl is not used.").Get()

	ValidationWebhookConfigName = env.Register("VALIDATION_WEBHOOK_CONFIG_NAME", "istio-istio-system",
		"If not empty, the controller will automatically patch validatingwebhookconfiguration when the CA certificate changes. "+
			"Only works in kubernetes environment.").Get()

	EnableXDSCaching = env.Register("PILOT_ENABLE_XDS_CACHE", true,
		"If true, Pilot will cache XDS responses.").Get()

	// EnableCDSCaching determines if CDS caching is enabled. This is explicitly split out of ENABLE_XDS_CACHE,
	// so that in case there are issues with the CDS cache we can just disable the CDS cache.
	EnableCDSCaching = env.Register("PILOT_ENABLE_CDS_CACHE", true,
		"If true, Pilot will cache CDS responses. Note: this depends on PILOT_ENABLE_XDS_CACHE.").Get()

	// EnableRDSCaching determines if RDS caching is enabled. This is explicitly split out of ENABLE_XDS_CACHE,
	// so that in case there are issues with the RDS cache we can just disable the RDS cache.
	EnableRDSCaching = env.Register("PILOT_ENABLE_RDS_CACHE", true,
		"If true, Pilot will cache RDS responses. Note: this depends on PILOT_ENABLE_XDS_CACHE.").Get()

	EnableXDSCacheMetrics = env.Register("PILOT_XDS_CACHE_STATS", false,
		"If true, Pilot will collect metrics for XDS cache efficiency.").Get()

	XDSCacheMaxSize = env.Register("PILOT_XDS_CACHE_SIZE", 60000,
		"The maximum number of cache entries for the XDS cache.").Get()

	XDSCacheIndexClearInterval = env.Register("PILOT_XDS_CACHE_INDEX_CLEAR_INTERVAL", 5*time.Second,
		"The interval for xds cache index clearing.").Get()

	XdsPushSendTimeout = env.Register(
		"PILOT_XDS_SEND_TIMEOUT",
		0*time.Second,
		"The timeout to send the XDS configuration to proxies. After this timeout is reached, Pilot will discard that push.",
	).Get()

	RemoteClusterTimeout = env.Register(
		"PILOT_REMOTE_CLUSTER_TIMEOUT",
		30*time.Second,
		"After this timeout expires, pilot can become ready without syncing data from clusters added via remote-secrets. "+
			"Setting the timeout to 0 disables this behavior.",
	).Get()

	EnableTelemetryLabel = env.Register("PILOT_ENABLE_TELEMETRY_LABEL", true,
		"If true, pilot will add telemetry related metadata to cluster and endpoint resources, which will be consumed by telemetry filter.",
	).Get()

	EndpointTelemetryLabel = env.Register("PILOT_ENDPOINT_TELEMETRY_LABEL", true,
		"If true, pilot will add telemetry related metadata to Endpoint resource, which will be consumed by telemetry filter.",
	).Get()

	MetadataExchange = env.Register("PILOT_ENABLE_METADATA_EXCHANGE", true,
		"If true, pilot will add metadata exchange filters, which will be consumed by telemetry filter.",
	).Get()

	ALPNFilter = env.Register("PILOT_ENABLE_ALPN_FILTER", true,
		"If true, pilot will add Istio ALPN filters, required for proper protocol sniffing.",
	).Get()

	WorkloadEntryAutoRegistration = env.Register("PILOT_ENABLE_WORKLOAD_ENTRY_AUTOREGISTRATION", true,
		"Enables auto-registering WorkloadEntries based on associated WorkloadGroups upon XDS connection by the workload.").Get()

	WorkloadEntryCleanupGracePeriod = env.Register("PILOT_WORKLOAD_ENTRY_GRACE_PERIOD", 10*time.Second,
		"The amount of time an auto-registered workload can remain disconnected from all Pilot instances before the "+
			"associated WorkloadEntry is cleaned up.").Get()

	WorkloadEntryHealthChecks = env.Register("PILOT_ENABLE_WORKLOAD_ENTRY_HEALTHCHECKS", true,
		"Enables automatic health checks of WorkloadEntries based on the config provided in the associated WorkloadGroup").Get()

	WorkloadEntryCrossCluster = env.Register("PILOT_ENABLE_CROSS_CLUSTER_WORKLOAD_ENTRY", true,
		"If enabled, pilot will read WorkloadEntry from other clusters, selectable by Services in that cluster.").Get()

	EnableDestinationRuleInheritance = env.Register(
		"PILOT_ENABLE_DESTINATION_RULE_INHERITANCE",
		false,
		"If set, workload specific DestinationRules will inherit configurations settings from mesh and namespace level rules",
	).Get()

	WasmRemoteLoadConversion = env.Register("ISTIO_AGENT_ENABLE_WASM_REMOTE_LOAD_CONVERSION", true,
		"If enabled, Istio agent will intercept ECDS resource update, downloads Wasm module, "+
			"and replaces Wasm module remote load with downloaded local module file.").Get()

	PilotJwtPubKeyRefreshInterval = env.Register(
		"PILOT_JWT_PUB_KEY_REFRESH_INTERVAL",
		20*time.Minute,
		"The interval for istiod to fetch the jwks_uri for the jwks public key.",
	).Get()

	EnableInboundPassthrough = env.Register(
		"PILOT_ENABLE_INBOUND_PASSTHROUGH",
		true,
		"If enabled, inbound clusters will be configured as ORIGINAL_DST clusters. When disabled, "+
			"requests are always sent to localhost. The primary implication of this is that when enabled, binding to POD_IP "+
			"will work while localhost will not; when disable, bind to POD_IP will not work, while localhost will. "+
			"The enabled behavior matches the behavior without Istio enabled at all; this flag exists only for backwards compatibility. "+
			"Regardless of this setting, the configuration can be overridden with the Sidecar.Ingress.DefaultEndpoint configuration.",
	).Get()

	// EnableHBONE provides a global Pilot flag for enabling HBONE.
	// Generally, this could be a per-proxy setting (and is, via ENABLE_HBONE node metadata).
	// However, there are some code paths that impact all clients, hence the global flag.
	// Warning: do not enable by default until endpoint_builder.go caching is fixed (and possibly other locations).
	EnableHBONE = env.Register(
		"PILOT_ENABLE_HBONE",
		false,
		"If enabled, HBONE support can be configured for proxies. "+
			"Note: proxies must opt in on a per-proxy basis with ENABLE_HBONE to actually get HBONE config, in addition to this flag.").Get()

	EnableAmbientControllers = env.Register(
		"PILOT_ENABLE_AMBIENT_CONTROLLERS",
		false,
		"If enabled, controllers required for ambient will run. This is required to run ambient mesh.").Get()

	// EnableUnsafeAssertions enables runtime checks to test assertions in our code. This should never be enabled in
	// production; when assertions fail Istio will panic.
	EnableUnsafeAssertions = env.Register(
		"UNSAFE_PILOT_ENABLE_RUNTIME_ASSERTIONS",
		false,
		"If enabled, addition runtime asserts will be performed. "+
			"These checks are both expensive and panic on failure. As a result, this should be used only for testing.",
	).Get()

	// EnableUnsafeDeltaTest enables runtime checks to test Delta XDS efficiency. This should never be enabled in
	// production.
	EnableUnsafeDeltaTest = env.Register(
		"UNSAFE_PILOT_ENABLE_DELTA_TEST",
		false,
		"If enabled, addition runtime tests for Delta XDS efficiency are added. "+
			"These checks are extremely expensive, so this should be used only for testing, not production.",
	).Get()

	DeltaXds = env.Register("ISTIO_DELTA_XDS", false,
		"If enabled, pilot will only send the delta configs as opposed to the state of the world on a "+
			"Resource Request. This feature uses the delta xds api, but does not currently send the actual deltas.").Get()

	SharedMeshConfig = env.Register("SHARED_MESH_CONFIG", "",
		"Additional config map to load for shared MeshConfig settings. The standard mesh config will take precedence.").Get()

	MultiRootMesh = env.Register("ISTIO_MULTIROOT_MESH", false,
		"If enabled, mesh will support certificates signed by more than one trustAnchor for ISTIO_MUTUAL mTLS").Get()

	EnableEnvoyFilterMetrics = env.Register("PILOT_ENVOY_FILTER_STATS", false,
		"If true, Pilot will collect metrics for envoy filter operations.").Get()

	EnableRouteCollapse = env.Register("PILOT_ENABLE_ROUTE_COLLAPSE_OPTIMIZATION", true,
		"If true, Pilot will merge virtual hosts with the same routes into a single virtual host, as an optimization.").Get()

	MulticlusterHeadlessEnabled = env.Register("ENABLE_MULTICLUSTER_HEADLESS", true,
		"If true, the DNS name table for a headless service will resolve to same-network endpoints in any cluster.").Get()

	ResolveHostnameGateways = env.Register("RESOLVE_HOSTNAME_GATEWAYS", true,
		"If true, hostnames in the LoadBalancer addresses of a Service will be resolved at the control plane for use in cross-network gateways.").Get()

	MultiNetworkGatewayAPI = env.Register("PILOT_MULTI_NETWORK_DISCOVER_GATEWAY_API", false,
		"If true, Pilot will discover labeled Kubernetes gateway objects as multi-network gateways.").Get()

	CertSignerDomain = env.Register("CERT_SIGNER_DOMAIN", "", "The cert signer domain info").Get()

	EnableQUICListeners = env.Register("PILOT_ENABLE_QUIC_LISTENERS", false,
		"If true, QUIC listeners will be generated wherever there are listeners terminating TLS on gateways "+
			"if the gateway service exposes a UDP port with the same number (for example 443/TCP and 443/UDP)").Get()

	VerifyCertAtClient = env.Register("VERIFY_CERTIFICATE_AT_CLIENT", false,
		"If enabled, certificates received by the proxy will be verified against the OS CA certificate bundle.").Get()

	EnableTLSOnSidecarIngress = env.Register("ENABLE_TLS_ON_SIDECAR_INGRESS", false,
		"If enabled, the TLS configuration on Sidecar.ingress will take effect").Get()

	EnableAutoSni = env.Register("ENABLE_AUTO_SNI", false,
		"If enabled, automatically set SNI when `DestinationRules` do not specify the same").Get()

	InsecureKubeConfigOptions = func() sets.String {
		v := env.Register(
			"PILOT_INSECURE_MULTICLUSTER_KUBECONFIG_OPTIONS",
			"",
			"Comma separated list of potentially insecure kubeconfig authentication options that are allowed for multicluster authentication."+
				"Support values: all authProviders (`gcp`, `azure`, `exec`, `openstack`), "+
				"`clientKey`, `clientCertificate`, `tokenFile`, and `exec`.").Get()
		return sets.New(strings.Split(v, ",")...)
	}()

	VerifySDSCertificate = env.Register("VERIFY_SDS_CERTIFICATE", true,
		"If enabled, certificates fetched from SDS server will be verified before sending back to proxy.").Get()

	EnableHCMInternalNetworks = env.Register("ENABLE_HCM_INTERNAL_NETWORKS", false,
		"If enable, endpoints defined in mesh networks will be configured as internal addresses in Http Connection Manager").Get()

	CanonicalServiceForMeshExternalServiceEntry = env.Register("LABEL_CANONICAL_SERVICES_FOR_MESH_EXTERNAL_SERVICE_ENTRIES", false,
		"If enabled, metadata representing canonical services for ServiceEntry resources with a location of mesh_external will be populated"+
			"in the cluster metadata for those endpoints.").Get()

	LocalClusterSecretWatcher = env.Register("LOCAL_CLUSTER_SECRET_WATCHER", false,
		"If enabled, the cluster secret watcher will watch the namespace of the external cluster instead of config cluster").Get()

	EnableEnhancedResourceScoping = env.Register("ENABLE_ENHANCED_RESOURCE_SCOPING", false,
		"If enabled, meshConfig.discoverySelectors will limit the CustomResource configurations(like Gateway,VirtualService,DestinationRule,Ingress, etc)"+
			"that can be processed by pilot. This will also restrict the root-ca certificate distribution.").Get()

	EnableLeaderElection = env.Register("ENABLE_LEADER_ELECTION", true,
		"If enabled (default), starts a leader election client and gains leadership before executing controllers. "+
			"If false, it assumes that only one instance of istiod is running and skips leader election.").Get()

	EnableSidecarServiceInboundListenerMerge = env.Register(
		"PILOT_ALLOW_SIDECAR_SERVICE_INBOUND_LISTENER_MERGE",
		false,
		"If set, it allows creating inbound listeners for service ports and sidecar ingress listeners ",
	).Get()

	EnableDualStack = env.RegisterBoolVar("ISTIO_DUAL_STACK", false,
		"If true, Istio will enable the Dual Stack feature.").Get()

	EnableOptimizedServicePush = env.RegisterBoolVar("ISTIO_ENABLE_OPTIMIZED_SERVICE_PUSH", true,
		"If enabled, Istiod will not push changes on arbitraty annotation change.").Get()

	InformerWatchNamespace = env.Register("ISTIO_WATCH_NAMESPACE", "",
		"If set, limit Kubernetes watches to a single namespace. "+
			"Warning: only a single namespace can be set.").Get()

	// This is a feature flag, can be removed if protobuf proves universally better.
	KubernetesClientContentType = env.Register("ISTIO_KUBE_CLIENT_CONTENT_TYPE", "protobuf",
		"The content type to use for Kubernetes clients. Defaults to protobuf. Valid options: [protobuf, json]").Get()

	// This is used in injection templates, it is not unused.
	EnableNativeSidecars = env.Register("ENABLE_NATIVE_SIDECARS", false,
		"If set, used Kubernetes native Sidecar container support. Requires SidecarContainer feature flag.")

	// This is an experimental feature flag, can be removed once it became stable, and should introduced to Telemetry API.
	MetricRotationInterval = env.Register("METRIC_ROTATION_INTERVAL", 0*time.Second,
		"Metric scope rotation interval, set to 0 to disable the metric scope rotation").Get()
	MetricGracefulDeletionInterval = env.Register("METRIC_GRACEFUL_DELETION_INTERVAL", 5*time.Minute,
		"Metric expiry graceful deletion interval. No-op if METRIC_ROTATION_INTERVAL is disabled.").Get()

	OptimizedConfigRebuild = env.Register("ENABLE_OPTIMIZED_CONFIG_REBUILD", true,
		"If enabled, pilot will only rebuild config for resources that have changed").Get()

	EnableControllerQueueMetrics = env.Register("ISTIO_ENABLE_CONTROLLER_QUEUE_METRICS", false,
		"If enabled, publishes metrics for queue depth, latency and processing times.").Get()

	JwksResolverInsecureSkipVerify = env.Register("JWKS_RESOLVER_INSECURE_SKIP_VERIFY", false,
		"If enabled, istiod will skip verifying the certificate of the JWKS server.").Get()
)

// UnsafeFeaturesEnabled returns true if any unsafe features are enabled.
func UnsafeFeaturesEnabled() bool {
	return EnableUnsafeAdminEndpoints || EnableUnsafeAssertions
}
