package features

import (
	"math"
	"time"

	"istio.io/istio/pkg/env"
)

var (
	WatchResourcesByNamespaceForPrimaryCluster = env.RegisterStringVar("WATCH_RESOURCES_BY_NAMESPACE_FOR_PRIMARY_CLUSTER", "", "Istio only watch"+
		"k8s resources in specified namespace.").Get()

	WatchResourcesByLabelForPrimaryCluster = env.RegisterStringVar("WATCH_RESOURCES_BY_LABEL_FOR_PRIMARY_CLUSTER", "", "Istio only watch"+
		"k8s resources in specified label.").Get()

	DefaultConfigSources = env.RegisterStringVar("DEFAULT_CONFIG_SOURCES", "", "add default config sources").Get()

	ADSCSyncTimeout = env.RegisterDurationVar(
		"ADSC_SYNC_TIMEOUT",
		10*time.Second,
		"After this time, adsc will reconnect the remote xds server if adsc hasn't been synced.",
	).Get()

	ShouldWatchConfigClusterServices = env.RegisterBoolVar("SHOULD_WATCH_CONFIG_CLUSTER_SERVICES", false, "Whether to watch services of config clsuter.").Get()

	ADSCMcpDelayDeleteEnabled = env.RegisterBoolVar("ADSC_MCP_DELAY_DELETE_ENABLED",
		true, "Whether to enable MCP delay delete.",
	).Get()

	ADSCMcpDelayDeleteDuration = env.RegisterDurationVar(
		"ADSC_MCP_DELAY_DELETE_DURATION",
		3*time.Minute,
		"The orphan MCP service will be actually deleted after the duration. The delete task will be queued until we reaches the delete timepoint, or will be deprecated if the service is updated again",
	).Get()

	FilterServiceSubscriptionList = env.RegisterBoolVar("PILOT_FILTER_SERVICE_SUBSCRIPTION_LIST",
		false,
		"if enabled, Pilot will send only cluster that referenced in serviceSubscriptionList CR",
	).Get()

	WebsocketEnabled = env.RegisterBoolVar("WEBSOCKET_ENABLED", true, "Whether to enable websocket by default.").Get()

	CustomCACertConfigMapName = env.RegisterStringVar("CUSTOM_CA_CERT_NAME", "",
		"Defines the configmap's name of  istio's root ca certificate").Get()

	HostRDSMergeSubset = env.RegisterBoolVar("HOST_RDS_MERGE_SUBSET", true,
		"If enabled, if host A is a subset of B, then we merge B's routes into A's hostRDS").Get()

	EnableScopedRDS = env.RegisterBoolVar("ENBALE_SCOPED_RDS", true,
		"If enabled, each host in virtualservice will have an independent RDS, which is used with SRDS").Get()

	OnDemandRDS = env.RegisterBoolVar("ON_DEMAND_RDS", false,
		"If enabled, the on demand filter will be added to the HCM filters").Get()

	DefaultUpstreamConcurrencyThreshold = env.RegisterIntVar("DEFAULT_UPSTREAM_CONCURRENCY_THRESHOLD", math.MaxUint32,
		"The default threshold of max_requests/max_pending_requests/max_connections of circuit breaker").Get()

	EnableLDSAuthnFilter = env.RegisterBoolVar("ENABLE_AUTHN_FILTER", false,
		"If enabled, the istio_authn filter will be append to LDS filter chain").Get()

	EnableLDSGrpcStatsFilter = env.RegisterBoolVar("ENABLE_GRPC_STATS_FILTER", false,
		"If enabled, the grpc_stats filter will be append to LDS filter chain").Get()
)
