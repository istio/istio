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
	"time"

	"istio.io/istio/pkg/jwt"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/pkg/env"
)

var (
	MaxConcurrentStreams = env.RegisterIntVar(
		"ISTIO_GPRC_MAXSTREAMS",
		100000,
		"Sets the maximum number of concurrent grpc streams.",
	).Get()

	TraceSampling = env.RegisterFloatVar(
		"PILOT_TRACE_SAMPLING",
		100.0,
		"Sets the mesh-wide trace sampling percentage. Should be 0.0 - 100.0. Precision to 0.01. "+
			"Default is 100, not recommended for production use.",
	).Get()

	PushThrottle = env.RegisterIntVar(
		"PILOT_PUSH_THROTTLE",
		100,
		"Limits the number of concurrent pushes allowed. On larger machines this can be increased for faster pushes",
	).Get()

	// MaxRecvMsgSize The max receive buffer size of gRPC received channel of Pilot in bytes.
	MaxRecvMsgSize = env.RegisterIntVar(
		"ISTIO_GPRC_MAXRECVMSGSIZE",
		4*1024*1024,
		"Sets the max receive buffer size of gRPC stream in bytes.",
	).Get()

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	// For larger clusters it can increase memory use and GC - useful for small tests.
	DebugConfigs = env.RegisterBoolVar("PILOT_DEBUG_ADSZ_CONFIG", false, "").Get()

	// FilterGatewayClusterConfig controls if a subset of clusters(only those required) should be pushed to gateways
	FilterGatewayClusterConfig = env.RegisterBoolVar("PILOT_FILTER_GATEWAY_CLUSTER_CONFIG", false, "").Get()

	DebounceAfter = env.RegisterDurationVar(
		"PILOT_DEBOUNCE_AFTER",
		100*time.Millisecond,
		"The delay added to config/registry events for debouncing. This will delay the push by "+
			"at least this internal. If no change is detected within this period, the push will happen, "+
			" otherwise we'll keep delaying until things settle, up to a max of PILOT_DEBOUNCE_MAX.",
	).Get()

	DebounceMax = env.RegisterDurationVar(
		"PILOT_DEBOUNCE_MAX",
		10*time.Second,
		"The maximum amount of time to wait for events while debouncing. If events keep showing up with no breaks "+
			"for this time, we'll trigger a push.",
	).Get()

	EnableEDSDebounce = env.RegisterBoolVar(
		"PILOT_ENABLE_EDS_DEBOUNCE",
		true,
		"If enabled, Pilot will include EDS pushes in the push debouncing, configured by PILOT_DEBOUNCE_AFTER and PILOT_DEBOUNCE_MAX."+
			" EDS pushes may be delayed, but there will be fewer pushes. By default this is enabled",
	)

	// HTTP10 will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	//
	// Alpha in 1.1, may become the default or be turned into a Sidecar API or mesh setting. Only applies to namespaces
	// where Sidecar is enabled.
	HTTP10 = env.RegisterBoolVar(
		"PILOT_HTTP10",
		false,
		"Enables the use of HTTP 1.0 in the outbound HTTP listeners, to support legacy applications.",
	).Get()

	initialFetchTimeoutVar = env.RegisterDurationVar(
		"PILOT_INITIAL_FETCH_TIMEOUT",
		0,
		"Specifies the initial_fetch_timeout for config. If this time is reached without "+
			"a response to the config requested by Envoy, the Envoy will move on with the init phase. "+
			"This prevents envoy from getting stuck waiting on config during startup.",
	)
	InitialFetchTimeout = func() *duration.Duration {
		timeout, f := initialFetchTimeoutVar.Lookup()
		if !f {
			return nil
		}
		return ptypes.DurationProto(timeout)
	}()

	TerminationDrainDuration = env.RegisterIntVar(
		"TERMINATION_DRAIN_DURATION_SECONDS",
		5,
		"The amount of time allowed for connections to complete on pilot-agent shutdown. "+
			"On receiving SIGTERM or SIGINT, pilot-agent tells the active Envoy to start draining, "+
			"preventing any new connections and allowing existing connections to complete. It then "+
			"sleeps for the TerminationDrainDuration and then kills any remaining active Envoy processes.",
	)

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_MYSQL_FILTER",
		false,
		"EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.",
	).Get()

	// EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `redis`.
	EnableRedisFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_REDIS_FILTER",
		false,
		"EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.",
	).Get()

	// UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners so that it picks up the localhost
	// address of the sender, which is an internal address, so that trusted headers are not sanitized.
	UseRemoteAddress = env.RegisterBoolVar(
		"PILOT_SIDECAR_USE_REMOTE_ADDRESS",
		false,
		"UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners.",
	).Get()

	// EnableThriftFilter enables injection of `envoy.filters.network.thrift_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `thrift`.
	EnableThriftFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_THRIFT_FILTER",
		false,
		"EnableThriftFilter enables injection of `envoy.filters.network.thrift_proxy` in the filter chain.",
	).Get()

	// SkipValidateTrustDomain tells the server proxy to not to check the peer's trust domain when
	// mTLS is enabled in authentication policy.
	SkipValidateTrustDomain = env.RegisterBoolVar(
		"PILOT_SKIP_VALIDATE_TRUST_DOMAIN",
		false,
		"Skip validating the peer is from the same trust domain when mTLS is enabled in authentication policy")

	EnableProtocolSniffingForOutbound = env.RegisterBoolVar(
		"PILOT_ENABLE_PROTOCOL_SNIFFING_FOR_OUTBOUND",
		true,
		"If enabled, protocol sniffing will be used for outbound listeners whose port protocol is not specified or unsupported",
	).Get()

	EnableProtocolSniffingForInbound = env.RegisterBoolVar(
		"PILOT_ENABLE_PROTOCOL_SNIFFING_FOR_INBOUND",
		true,
		"If enabled, protocol sniffing will be used for inbound listeners whose port protocol is not specified or unsupported",
	).Get()

	EnableTCPMetadataExchange = env.RegisterBoolVar(
		"PILOT_ENABLE_TCP_METADATA_EXCHANGE",
		true,
		"If enabled, metadata exchange will be enabled for TCP using ALPN and Network Metadata Exchange filters in Envoy",
	).Get()

	ScopeGatewayToNamespace = env.RegisterBoolVar(
		"PILOT_SCOPE_GATEWAY_TO_NAMESPACE",
		false,
		"If enabled, a gateway workload can only select gateway resources in the same namespace. "+
			"Gateways with same selectors in different namespaces will not be applicable.",
	).Get()

	InboundProtocolDetectionTimeout = env.RegisterDurationVar(
		"PILOT_INBOUND_PROTOCOL_DETECTION_TIMEOUT",
		1*time.Second,
		"Protocol detection timeout for inbound listener",
	).Get()

	EnableHeadlessService = env.RegisterBoolVar(
		"PILOT_ENABLE_HEADLESS_SERVICE_POD_LISTENERS",
		true,
		"If enabled, for a headless service/stateful set in Kubernetes, pilot will generate an "+
			"outbound listener for each pod in a headless service. This feature should be disabled "+
			"if headless services have a large number of pods.",
	).Get()

	EnableEDSForHeadless = env.RegisterBoolVar(
		"PILOT_ENABLE_EDS_FOR_HEADLESS_SERVICES",
		false,
		"If enabled, for headless service in Kubernetes, pilot will send endpoints over EDS, "+
			"allowing the sidecar to load balance among pods in the headless service. This feature "+
			"should be enabled if applications access all services explicitly via a HTTP proxy port in the sidecar.",
	).Get()

	EnableDistributionTracking = env.RegisterBoolVar(
		"PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING",
		true,
		"If enabled, Pilot will assign meaningful nonces to each Envoy configuration message, and allow "+
			"users to interrogate which envoy has which config from the debug interface.",
	).Get()

	DistributionHistoryRetention = env.RegisterDurationVar(
		"PILOT_DISTRIBUTION_HISTORY_RETENTION",
		time.Minute*1,
		"If enabled, Pilot will keep track of old versions of distributed config for this duration.",
	).Get()

	EnableEndpointSliceController = env.RegisterBoolVar(
		"PILOT_USE_ENDPOINT_SLICE",
		false,
		"If enabled, Pilot will use EndpointSlices as the source of endpoints for Kubernetes services. "+
			"By default, this is false, and Endpoints will be used. This requires the Kubernetes EndpointSlice controller to be enabled. "+
			"Currently this is mutual exclusive - either Endpoints or EndpointSlices will be used",
	).Get()

	EnableCRDValidation = env.RegisterBoolVar(
		"PILOT_ENABLE_CRD_VALIDATION",
		false,
		"If enabled, pilot will validate CRDs while retrieving CRDs from kubernetes cache."+
			"Use this flag to enable validation of CRDs in Pilot, especially in deployments "+
			"that do not have galley installed.",
	).Get()

	EnableAnalysis = env.RegisterBoolVar(
		"PILOT_ENABLE_ANALYSIS",
		false,
		"If enabled, pilot will run istio analyzers and write analysis errors to the Status field of any "+
			"Istio Resources",
	).Get()

	EnableStatus = env.RegisterBoolVar(
		"PILOT_ENABLE_STATUS",
		false,
		"If enabled, pilot will update the CRD Status field of all istio resources with reconciliation status.",
	).Get()

	StatusQPS = env.RegisterFloatVar(
		"PILOT_STATUS_QPS",
		100,
		"If status is enabled, controls the QPS with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config QPS",
	).Get()

	StatusBurst = env.RegisterIntVar(
		"PILOT_STATUS_BURST",
		500,
		"If status is enabled, controls the Burst rate with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config Burst",
	).Get()

	// IstiodServiceCustomHost allow user to bring a custom address for istiod server
	// for examples: istiod.mycompany.com
	IstiodServiceCustomHost = env.RegisterStringVar("ISTIOD_CUSTOM_HOST", "",
		"Custom host name of istiod that istiod signs the server cert.")

	PilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"the provider of Pilot DNS certificate.")

	JwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.")

	// Default request timeout for virtual services if a timeout is not configured in virtual service. It defaults to zero
	// which disables timeout when it is not configured, to preserve the current behavior.
	defaultRequestTimeoutVar = env.RegisterDurationVar(
		"ISTIO_DEFAULT_REQUEST_TIMEOUT",
		0*time.Millisecond,
		"Default Http and gRPC Request timeout",
	)

	DefaultRequestTimeout = func() *duration.Duration {
		return ptypes.DurationProto(defaultRequestTimeoutVar.Get())
	}()

	EnableServiceApis = env.RegisterBoolVar("PILOT_ENABLED_SERVICE_APIS", false,
		"If this is set to true, support for Kubernetes service-apis (github.com/kubernetes-sigs/service-apis) will "+
			" be enabled. This feature is currently experimental, and is off by default.").Get()

	EnableVirtualServiceDelegate = env.RegisterBoolVar(
		"PILOT_ENABLE_VIRTUAL_SERVICE_DELEGATE",
		false,
		"If enabled, Pilot will merge virtual services with delegates. "+
			"By default, this is false, and virtualService with delegate will be ignored",
	).Get()

	ClusterName = env.RegisterStringVar("CLUSTER_ID", "Kubernetes",
		"Defines the cluster and service registry that this Istiod instance is belongs to").Get()

	EnableIncrementalMCP = env.RegisterBoolVar(
		"PILOT_ENABLE_INCREMENTAL_MCP",
		false,
		"If enabled, pilot will set the incremental flag of the options in the mcp controller "+
			"to true, and then galley may push data incrementally, it depends on whether the "+
			"resource supports incremental. By default, this is false.").Get()

	CentralIstioD = env.RegisterBoolVar("CENTRAL_ISTIOD", false,
		"If this is set to true, one Istiod will control remote clusters including CA.").Get()

	EnableCAServer = env.RegisterBoolVar("ENABLE_CA_SERVER", true,
		"If this is set to false, will not create CA server in istiod.").Get()

	XDSAuth = env.RegisterBoolVar("XDS_AUTH", true,
		"If true, will authenticate XDS clients.").Get()

	EnableServiceEntrySelectPods = env.RegisterBoolVar("PILOT_ENABLE_SERVICEENTRY_SELECT_PODS", true,
		"If enabled, service entries with selectors will select pods from the cluster. "+
			"It is safe to disable it if you are quite sure you don't need this feature").Get()
	EnableK8SServiceSelectWorkloadEntries = env.RegisterBoolVar("PILOT_ENABLE_K8S_SELECT_WORKLOAD_ENTRIES", true,
		"If enabled, Kubernetes services with selectors will select workload entries with matching labels. "+
			"It is safe to disable it if you are quite sure you don't need this feature").Get()
	InjectionWebhookConfigName = env.RegisterStringVar("INJECTION_WEBHOOK_CONFIG_NAME", "istio-sidecar-injector",
		"Name of the mutatingwebhookconfiguration to patch, if istioctl is not used.")

	SpiffeBundleEndpoints = env.RegisterStringVar("SPIFFE_BUNDLE_ENDPOINTS", "",
		"The SPIFFE bundle trust domain to endpoint mappings. Istiod retrieves the root certificate from each SPIFFE "+
			"bundle endpoint and uses it to verify client certifiates from that trust domain. The endpoint must be "+
			"compliant to the SPIFFE Bundle Endpoint standard. For details, please refer to "+
			"https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE_Trust_Domain_and_Bundle.md . "+
			"No need to configure this for root certificates issued via Istiod or web-PKI based root certificates. "+
			"Use || between <trustdomain, endpoint> tuples. Use | as delimiter between trust domain and endpoint in "+
			"each tuple. For example: foo|https://url/for/foo||bar|https://url/for/bar").Get()

	EnableTLSv2OnInboundPath = env.RegisterBoolVar("PILOT_SIDECAR_ENABLE_INBOUND_TLS_V2", false,
		"If true, Pilot will set the TLS version on server side as TLSv1_2 and also enforce strong cipher suites").Get()
)
