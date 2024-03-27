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

	"go.uber.org/atomic"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
)

// Define experimental features here.
var (
	// FilterGatewayClusterConfig controls if a subset of clusters(only those required) should be pushed to gateways
	FilterGatewayClusterConfig = env.Register("PILOT_FILTER_GATEWAY_CLUSTER_CONFIG", false,
		"If enabled, Pilot will send only clusters that referenced in gateway virtual services attached to gateway").Get()

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
		"The version to be used for the Kubernetes Multi-Cluster Services (MCS) API.").Get()

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

	EnableGatewayAPIGatewayClassController = env.Register("PILOT_ENABLE_GATEWAY_API_GATEWAYCLASS_CONTROLLER", true,
		"If this is set to true, istiod will create and manage its default GatewayClasses").Get()

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

	EnableAmbientWaypoints = func() bool {
		v := env.Register(
			"PILOT_ENABLE_AMBIENT_WAYPOINTS",
			false,
			"If enabled, controllers required for ambient will run. This is required to run ambient mesh.").Get()
		if v && !EnableAmbientControllers {
			log.Fatalf("PILOT_ENABLE_AMBIENT_WAYPOINTS requires PILOT_ENABLE_AMBIENT_CONTROLLERS")
		}
		return v
	}()

	DeltaXds = env.Register("ISTIO_DELTA_XDS", true,
		"If enabled, pilot will only send the delta configs as opposed to the state of the world on a "+
			"Resource Request. This feature uses the delta xds api, but does not currently send the actual deltas.").Get()

	EnableQUICListeners = env.Register("PILOT_ENABLE_QUIC_LISTENERS", false,
		"If true, QUIC listeners will be generated wherever there are listeners terminating TLS on gateways "+
			"if the gateway service exposes a UDP port with the same number (for example 443/TCP and 443/UDP)").Get()

	EnableTLSOnSidecarIngress = env.Register("ENABLE_TLS_ON_SIDECAR_INGRESS", false,
		"If enabled, the TLS configuration on Sidecar.ingress will take effect").Get()

	VerifySDSCertificate = env.Register("VERIFY_SDS_CERTIFICATE", true,
		"If enabled, certificates fetched from SDS server will be verified before sending back to proxy.").Get()

	EnableHCMInternalNetworks = env.Register("ENABLE_HCM_INTERNAL_NETWORKS", false,
		"If enable, endpoints defined in mesh networks will be configured as internal addresses in Http Connection Manager").Get()

	EnableEnhancedResourceScoping = env.Register("ENABLE_ENHANCED_RESOURCE_SCOPING", true,
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
		"If enabled, Istiod will not push changes on arbitrary annotation change.").Get()

	// This is used in injection templates, it is not unused.
	EnableNativeSidecars = env.Register("ENABLE_NATIVE_SIDECARS", false,
		"If set, used Kubernetes native Sidecar container support. Requires SidecarContainer feature flag.")

	OptimizedConfigRebuild = env.Register("ENABLE_OPTIMIZED_CONFIG_REBUILD", true,
		"If enabled, pilot will only rebuild config for resources that have changed").Get()

	PassthroughTargetPort = env.Register("ENABLE_RESOLUTION_NONE_TARGET_PORT", true,
		"If enabled, targetPort will be supported for resolution=NONE ServiceEntry").Get()

	PersistOldestWinsHeuristicForVirtualServiceHostMatching = env.Register("PERSIST_OLDEST_FIRST_HEURISTIC_FOR_VIRTUAL_SERVICE_HOST_MATCHING", false,
		"If enabled, istiod will persist the oldest first heuristic for subtly conflicting traffic policy selection"+
			"(such as with overlapping wildcard hosts)").Get()
)
