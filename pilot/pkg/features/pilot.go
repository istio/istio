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

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/util/sets"
)

var (
	// HTTP10 will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	HTTP10 = env.Register(
		"PILOT_HTTP10",
		false,
		"Enables the use of HTTP 1.0 in the outbound HTTP listeners, to support legacy applications.",
	).Get()

	ScopeGatewayToNamespace = env.Register(
		"PILOT_SCOPE_GATEWAY_TO_NAMESPACE",
		false,
		"If enabled, a gateway workload can only select gateway resources in the same namespace. "+
			"Gateways with same selectors in different namespaces will not be applicable.",
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

	// IstiodServiceCustomHost allow user to bring a custom address or multiple custom addresses for istiod server
	// for examples: 1. istiod.mycompany.com  2. istiod.mycompany.com,istiod-canary.mycompany.com
	IstiodServiceCustomHost = env.Register("ISTIOD_CUSTOM_HOST", "",
		"Custom host name of istiod that istiod signs the server cert. "+
			"Multiple custom host names are supported, and multiple values are separated by commas.").Get()

	PilotCertProvider = env.Register("PILOT_CERT_PROVIDER", constants.CertProviderIstiod,
		"The provider of Pilot DNS certificate. K8S RA will be used for k8s.io/NAME. 'istiod' value will sign"+
			" using Istio build in CA. Other values will not not generate TLS certs, but still "+
			" distribute ./etc/certs/root-cert.pem. Only used if custom certificates are not mounted.").Get()

	ClusterName = env.Register("CLUSTER_ID", constants.DefaultClusterName,
		"Defines the cluster and service registry that this Istiod instance belongs to").Get()

	// This value defaults to 0 which is interpreted as infinity and causes Envoys to get "stuck" to dead dns pods
	// (https://github.com/istio/istio/issues/53577).
	// However, setting this value too high will rate limit the number of DNS requests we can make by using up all
	// available local udp ports.
	// Generally, the max dns query rate is
	//
	//	(# local dns ports) * (udp_max_queries) / (query duration)
	//
	// The longest a query can take should be 5s, the Envoy default timeout.
	// We underestimate the number of local dns ports to 10,000 (default is ~15,000, but some might be in use).
	// Setting udp_max_queries to 100 gives us at least 10,000 * 100 / 5 = 200,000 requests / second, which is
	// hopefully enough for any cluster.
	PilotDNSCaresUDPMaxQueries = env.Register("PILOT_DNS_CARES_UDP_MAX_QUERIES", uint32(100),
		"Sets the `udp_max_queries` option in Envoy for the Cares DNS resolver. "+
			"Defaults to 0, an unlimited number of queries. "+
			"See `extensions.network.dns_resolver.cares.v3.CaresDnsResolverConfig` in "+
			"https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/network/dns_resolver/cares/v3/cares_dns_resolver.proto "+
			"and `ARES_OPT_UDP_MAX_QUERIES` in https://c-ares.org/docs/ares_init.html").Get()

	PilotDNSJitterDurationEnv = env.Register("PILOT_DNS_JITTER_DURATION", 100*time.Millisecond, "Jitter added to periodic DNS resolution").Get()

	ExternalIstiod = env.Register("EXTERNAL_ISTIOD", false,
		"If this is set to true, one Istiod will control remote clusters including CA.").Get()

	EnableCAServer = env.Register("ENABLE_CA_SERVER", true,
		"If this is set to false, will not create CA server in istiod.").Get()

	EnableDebugOnHTTP = env.Register("ENABLE_DEBUG_ON_HTTP", true,
		"If this is set to false, the debug interface will not be enabled, recommended for production").Get()

	EnableUnsafeAdminEndpoints = env.Register("UNSAFE_ENABLE_ADMIN_ENDPOINTS", false,
		"If this is set to true, dangerous admin endpoints will be exposed on the debug interface. Not recommended for production.").Get()

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

	RemoteClusterTimeout = env.Register(
		"PILOT_REMOTE_CLUSTER_TIMEOUT",
		30*time.Second,
		"After this timeout expires, pilot can become ready without syncing data from clusters added via remote-secrets. "+
			"Setting the timeout to 0 disables this behavior.",
	).Get()

	DisableMxALPN = env.Register("PILOT_DISABLE_MX_ALPN", false,
		"If true, pilot will not put istio-peer-exchange ALPN into TLS handshake configuration.",
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

	WasmRemoteLoadConversion = env.Register("ISTIO_AGENT_ENABLE_WASM_REMOTE_LOAD_CONVERSION", true,
		"If enabled, Istio agent will intercept ECDS resource update, downloads Wasm module, "+
			"and replaces Wasm module remote load with downloaded local module file.").Get()

	PilotJwtPubKeyRefreshInterval = env.Register(
		"PILOT_JWT_PUB_KEY_REFRESH_INTERVAL",
		20*time.Minute,
		"The interval for istiod to fetch the jwks_uri for the jwks public key.",
	).Get()

	EnableNodeUntaintControllers = env.Register(
		"PILOT_ENABLE_NODE_UNTAINT_CONTROLLERS",
		false,
		"If enabled, controller that untaints nodes with cni pods ready will run. This should be enabled if you disabled ambient init containers.").Get()

	EnableIPAutoallocate = env.Register(
		"PILOT_ENABLE_IP_AUTOALLOCATE",
		true,
		"If enabled, pilot will start a controller that assigns IP addresses to ServiceEntry which do not have a user-supplied IP. "+
			"This, when combined with DNS capture allows for tcp routing of traffic sent to the ServiceEntry.").Get()

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

	MultiNetworkGatewayAPI = env.Register("PILOT_MULTI_NETWORK_DISCOVER_GATEWAY_API", true,
		"If true, Pilot will discover labeled Kubernetes gateway objects as multi-network gateways.").Get()

	InsecureKubeConfigOptions = func() sets.String {
		v := env.Register(
			"PILOT_INSECURE_MULTICLUSTER_KUBECONFIG_OPTIONS",
			"",
			"Comma separated list of potentially insecure kubeconfig authentication options that are allowed for multicluster authentication."+
				"Support values: all authProviders (`gcp`, `azure`, `exec`, `openstack`), "+
				"`clientKey`, `clientCertificate`, `tokenFile`, and `exec`.").Get()
		return sets.New(strings.Split(v, ",")...)
	}()

	CanonicalServiceForMeshExternalServiceEntry = env.Register("LABEL_CANONICAL_SERVICES_FOR_MESH_EXTERNAL_SERVICE_ENTRIES", false,
		"If enabled, metadata representing canonical services for ServiceEntry resources with a location of mesh_external will be populated"+
			"in the cluster metadata for those endpoints.").Get()

	LocalClusterSecretWatcher = env.Register("LOCAL_CLUSTER_SECRET_WATCHER", false,
		"If enabled, the cluster secret watcher will watch the namespace of the external cluster instead of config cluster").Get()

	InformerWatchNamespace = env.Register("ISTIO_WATCH_NAMESPACE", "",
		"If set, limit Kubernetes watches to a single namespace. "+
			"Warning: only a single namespace can be set.").Get()

	// This is a feature flag, can be removed if protobuf proves universally better.
	KubernetesClientContentType = env.Register("ISTIO_KUBE_CLIENT_CONTENT_TYPE", "protobuf",
		"The content type to use for Kubernetes clients. Defaults to protobuf. Valid options: [protobuf, json]").Get()

	ValidateWorkloadEntryIdentity = env.Register("ISTIO_WORKLOAD_ENTRY_VALIDATE_IDENTITY", true,
		"If enabled, will validate the identity of a workload matches the identity of the "+
			"WorkloadEntry it is associating with for health checks and auto registration. "+
			"This flag is added for backwards compatibility only and will be removed in future releases").Get()

	JwksResolverInsecureSkipVerify = env.Register("JWKS_RESOLVER_INSECURE_SKIP_VERIFY", false,
		"If enabled, istiod will skip verifying the certificate of the JWKS server.").Get()

	EnableSelectorBasedK8sGatewayPolicy = env.Register("ENABLE_SELECTOR_BASED_K8S_GATEWAY_POLICY", true,
		"If disabled, Gateway API gateways will ignore workloadSelector policies, only"+
			"applying policies that select the gateway with a targetRef.").Get()

	// Useful for IPv6-only EKS clusters. See https://aws.github.io/aws-eks-best-practices/networking/ipv6/ why it assigns an additional IPv4 NAT address.
	// Also see https://github.com/istio/istio/issues/46719 why this flag is required
	EnableAdditionalIpv4OutboundListenerForIpv6Only = env.RegisterBoolVar("ISTIO_ENABLE_IPV4_OUTBOUND_LISTENER_FOR_IPV6_CLUSTERS", false,
		"If true, pilot will configure an additional IPv4 listener for outbound traffic in IPv6 only clusters, e.g. AWS EKS IPv6 only clusters.").Get()

	EnableVtprotobuf = env.Register("ENABLE_VTPROTOBUF", true,
		"If true, will use optimized vtprotobuf based marshaling. Requires a build with -tags=vtprotobuf.").Get()

	GatewayAPIDefaultGatewayClass = env.Register("PILOT_GATEWAY_API_DEFAULT_GATEWAYCLASS_NAME", "istio",
		"Name of the default GatewayClass").Get()

	ManagedGatewayController = env.Register("PILOT_GATEWAY_API_CONTROLLER_NAME", "istio.io/gateway-controller",
		"Gateway API controller name. istiod will only reconcile Gateway API resources referencing a GatewayClass with this controller name").Get()

	PreferDestinationRulesTLSForExternalServices = env.Register("PREFER_DESTINATIONRULE_TLS_FOR_EXTERNAL_SERVICES", true,
		"If true, external services will prefer the TLS settings from DestinationRules over the metadata TLS settings.").Get()

	EnableGatewayAPIManualDeployment = env.Register("ENABLE_GATEWAY_API_MANUAL_DEPLOYMENT", true,
		"If true, allows users to bind Gateway API resources to existing gateway deployments.").Get()

	MaxConnectionsToAcceptPerSocketEvent = env.Register("MAX_CONNECTIONS_PER_SOCKET_EVENT_LOOP", 1,
		"The maximum number of connections to accept from the kernel per socket event. Set this to '0' to accept unlimited connections.").Get()

	EnableClusterTrustBundles = env.Register("ENABLE_CLUSTER_TRUST_BUNDLE_API", false,
		"If enabled, uses the ClusterTrustBundle API instead of ConfigMaps to store the root certificate in the cluster.").Get()

	// EnableAbsoluteFqdnVhostDomain controls whether the absolute FQDN (hostname followed by a dot,)
	// e.g. my-service.my-ns.svc.cluster.local. / google.com. is added to the VirtualHost domains list.
	// Setting this to false disables the addition.
	// See https://github.com/istio/istio/issues/56007 for more details of this feature with examples.
	EnableAbsoluteFqdnVhostDomain = env.Register(
		"PILOT_ENABLE_ABSOLUTE_FQDN_VHOST_DOMAIN", // Environment variable name
		true, // Default value (true = feature enabled by default)
		"If set to false, Istio will not add the absolute FQDN variant"+
			" (e.g., my-service.my-ns.svc.cluster.local.) to the domains"+
			" list for VirtualHost entries.",
	).Get()

	EnableProxyFindPodByIP = env.Register("ENABLE_PROXY_FIND_POD_BY_IP", false,
		"If enabled, the pod controller will allow finding pods matching proxies by IP if it fails to find them by name.").Get()
)

// UnsafeFeaturesEnabled returns true if any unsafe features are enabled.
func UnsafeFeaturesEnabled() bool {
	return EnableUnsafeAdminEndpoints || EnableUnsafeAssertions || EnableUnsafeDeltaTest
}
