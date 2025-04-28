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

package model

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"

	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
	"istio.io/istio/pkg/xds"
)

// Metrics is an interface for capturing metrics on a per-node basis.
type Metrics interface {
	// AddMetric will add an case to the metric for the given node.
	AddMetric(metric monitoring.Metric, key string, proxyID, msg string)
}

var _ Metrics = &PushContext{}

// serviceIndex is an index of all services by various fields for easy access during push.
type serviceIndex struct {
	// privateByNamespace are services that can reachable within the same namespace, with exportTo "."
	privateByNamespace map[string][]*Service
	// public are services reachable within the mesh with exportTo "*"
	public []*Service
	// exportedToNamespace are services that were made visible to this namespace
	// by an exportTo explicitly specifying this namespace.
	exportedToNamespace map[string][]*Service

	// HostnameAndNamespace has all services, indexed by hostname then namespace.
	HostnameAndNamespace map[host.Name]map[string]*Service `json:"-"`

	// instancesByPort contains a map of service key and instances by port. It is stored here
	// to avoid recomputations during push. This caches instanceByPort calls with empty labels.
	// Call InstancesByPort directly when instances need to be filtered by actual labels.
	instancesByPort map[string]map[int][]*IstioEndpoint
}

func newServiceIndex() serviceIndex {
	return serviceIndex{
		public:               []*Service{},
		privateByNamespace:   map[string][]*Service{},
		exportedToNamespace:  map[string][]*Service{},
		HostnameAndNamespace: map[host.Name]map[string]*Service{},
		instancesByPort:      map[string]map[int][]*IstioEndpoint{},
	}
}

// exportToDefaults contains the default exportTo values.
type exportToDefaults struct {
	service         sets.Set[visibility.Instance]
	virtualService  sets.Set[visibility.Instance]
	destinationRule sets.Set[visibility.Instance]
}

// virtualServiceIndex is the index of virtual services by various fields.
type virtualServiceIndex struct {
	exportedToNamespaceByGateway map[types.NamespacedName][]config.Config
	// this contains all the virtual services with exportTo "." and current namespace. The keys are namespace,gateway.
	privateByNamespaceAndGateway map[types.NamespacedName][]config.Config
	// This contains all virtual services whose exportTo is "*", keyed by gateway
	publicByGateway map[string][]config.Config
	// root vs namespace/name ->delegate vs virtualservice gvk/namespace/name
	delegates map[ConfigKey][]ConfigKey

	// This contains destination hosts of virtual services, keyed by gateway's namespace/name,
	// only used when PILOT_FILTER_GATEWAY_CLUSTER_CONFIG is enabled
	destinationsByGateway map[string]sets.String

	// Map of VS hostname -> referenced hostnames
	referencedDestinations map[string]sets.String
}

func newVirtualServiceIndex() virtualServiceIndex {
	out := virtualServiceIndex{
		publicByGateway:              map[string][]config.Config{},
		privateByNamespaceAndGateway: map[types.NamespacedName][]config.Config{},
		exportedToNamespaceByGateway: map[types.NamespacedName][]config.Config{},
		delegates:                    map[ConfigKey][]ConfigKey{},
		referencedDestinations:       map[string]sets.String{},
	}
	if features.FilterGatewayClusterConfig {
		out.destinationsByGateway = make(map[string]sets.String)
	}
	return out
}

// destinationRuleIndex is the index of destination rules by various fields.
type destinationRuleIndex struct {
	//  namespaceLocal contains all public/private dest rules pertaining to a service defined in a given namespace.
	namespaceLocal map[string]*consolidatedDestRules
	//  exportedByNamespace contains all dest rules pertaining to a service exported by a namespace.
	exportedByNamespace map[string]*consolidatedDestRules
	rootNamespaceLocal  *consolidatedDestRules
}

func newDestinationRuleIndex() destinationRuleIndex {
	return destinationRuleIndex{
		namespaceLocal:      map[string]*consolidatedDestRules{},
		exportedByNamespace: map[string]*consolidatedDestRules{},
	}
}

// sidecarIndex is the index of sidecar rules
type sidecarIndex struct {
	// user configured sidecars for each namespace if available.
	sidecarsByNamespace map[string][]*SidecarScope
	// the Sidecar for the root namespace (if present). This applies to any namespace without its own Sidecar.
	meshRootSidecarConfig *config.Config
	// meshRootSidecarsByNamespace contains the default sidecar for namespaces that do not have a sidecar.
	// These are converted from root namespace sidecar if it exists.
	// These are lazy-loaded. Access protected by derivedSidecarMutex.
	meshRootSidecarsByNamespace map[string]*SidecarScope
	// defaultSidecarsByNamespace contains the default sidecar for namespaces that do not have a sidecar,
	// These are *always* computed from DefaultSidecarScopeForNamespace i.e. a sidecar that has listeners
	// for all services in the mesh. This will be used if there is no sidecar specified in root namespace.
	// These are lazy-loaded. Access protected by derivedSidecarMutex.
	defaultSidecarsByNamespace map[string]*SidecarScope
	// sidecarsForGatewayByNamespace contains the default sidecar for gateways and waypoints,
	// These are *always* computed from DefaultSidecarScopeForGateway.
	// These are lazy-loaded. Access protected by derivedSidecarMutex.
	sidecarsForGatewayByNamespace map[string]*SidecarScope

	// mutex to protect derived sidecars i.e. not specified by user.
	derivedSidecarMutex *sync.RWMutex
}

func newSidecarIndex() sidecarIndex {
	return sidecarIndex{
		sidecarsByNamespace:           map[string][]*SidecarScope{},
		meshRootSidecarsByNamespace:   map[string]*SidecarScope{},
		defaultSidecarsByNamespace:    map[string]*SidecarScope{},
		sidecarsForGatewayByNamespace: map[string]*SidecarScope{},
		derivedSidecarMutex:           &sync.RWMutex{},
	}
}

// gatewayIndex is the index of gateways by various fields.
type gatewayIndex struct {
	// namespace contains gateways by namespace.
	namespace map[string][]config.Config
	// all contains all gateways.
	all []config.Config
}

func newGatewayIndex() gatewayIndex {
	return gatewayIndex{
		namespace: map[string][]config.Config{},
		all:       []config.Config{},
	}
}

type serviceAccountKey struct {
	hostname  host.Name
	namespace string
}

// PushContext tracks the status of a push - metrics and errors.
// Metrics are reset after a push - at the beginning all
// values are zero, and when push completes the status is reset.
// The struct is exposed in a debug endpoint - fields public to allow
// easy serialization as json.
type PushContext struct {
	proxyStatusMutex sync.RWMutex
	// ProxyStatus is keyed by the error code, and holds a map keyed
	// by the ID.
	ProxyStatus map[string]map[string]ProxyPushStatus

	// Synthesized from env.Mesh
	exportToDefaults exportToDefaults

	// ServiceIndex is the index of services by various fields.
	ServiceIndex serviceIndex

	// serviceAccounts contains a map of hostname and port to service accounts.
	serviceAccounts map[serviceAccountKey][]string

	// virtualServiceIndex is the index of virtual services by various fields.
	virtualServiceIndex virtualServiceIndex

	// destinationRuleIndex is the index of destination rules by various fields.
	destinationRuleIndex destinationRuleIndex

	// gatewayIndex is the index of gateways.
	gatewayIndex gatewayIndex

	// clusterLocalHosts extracted from the MeshConfig
	clusterLocalHosts ClusterLocalHosts

	// sidecarIndex stores sidecar resources
	sidecarIndex sidecarIndex

	// envoy filters for each namespace including global config namespace
	envoyFiltersByNamespace map[string][]*EnvoyFilterWrapper

	// wasm plugins for each namespace including global config namespace
	wasmPluginsByNamespace map[string][]*WasmPluginWrapper

	// AuthnPolicies contains Authn policies by namespace.
	AuthnPolicies *AuthenticationPolicies `json:"-"`

	// AuthzPolicies stores the existing authorization policies in the cluster. Could be nil if there
	// are no authorization policies in the cluster.
	AuthzPolicies *AuthorizationPolicies `json:"-"`

	// Telemetry stores the existing Telemetry resources for the cluster.
	Telemetry *Telemetries `json:"-"`

	// ProxyConfig stores the existing ProxyConfig resources for the cluster.
	ProxyConfigs *ProxyConfigs `json:"-"`

	// The following data is either a global index or used in the inbound path.
	// Namespace specific views do not apply here.

	// Mesh configuration for the mesh.
	Mesh *meshconfig.MeshConfig `json:"-"`

	// PushVersion describes the push version this push context was computed for
	PushVersion string

	// JwtKeyResolver holds a reference to the JWT key resolver instance.
	JwtKeyResolver *JwksResolver

	// GatewayAPIController holds a reference to the gateway API controller.
	GatewayAPIController GatewayController

	// cache gateways addresses for each network
	// this is mainly used for kubernetes multi-cluster scenario
	networkMgr *NetworkManager

	Networks *meshconfig.MeshNetworks

	InitDone        atomic.Bool
	initializeMutex sync.Mutex
	ambientIndex    AmbientIndexes
}

type consolidatedDestRules struct {
	// Map of dest rule host and the merged destination rules for that host.
	// Only stores specific non-wildcard destination rules
	specificDestRules map[host.Name][]*ConsolidatedDestRule
	// Map of dest rule host and the merged destination rules for that host.
	// Only stores wildcard destination rules
	wildcardDestRules map[host.Name][]*ConsolidatedDestRule
}

// ConsolidatedDestRule represents a dr and from which it is consolidated.
type ConsolidatedDestRule struct {
	// the list of namespaces to which this destination rule has been exported to
	exportTo sets.Set[visibility.Instance]
	// rule is merged from the following destinationRules.
	rule *config.Config
	// the original dest rules from which above rule is merged.
	from []types.NamespacedName
}

// MarshalJSON implements json.Marshaller
func (l *ConsolidatedDestRule) MarshalJSON() ([]byte, error) {
	// Json cannot expose unexported fields, so copy the ones we want here
	return json.MarshalIndent(map[string]any{
		"exportTo": l.exportTo,
		"rule":     l.rule,
		"from":     l.from,
	}, "", "  ")
}

type EdsUpdateFn func(shard ShardKey, hostname string, namespace string, entry []*IstioEndpoint)

// XDSUpdater is used for direct updates of the xDS model and incremental push.
// Pilot uses multiple registries - for example each K8S cluster is a registry
// instance. Each registry is responsible for tracking a set
// of endpoints associated with mesh services, and calling the EDSUpdate on changes.
// A registry may group endpoints for a service in smaller subsets - for example by
// deployment, or to deal with very large number of endpoints for a service. We want
// to avoid passing around large objects - like full list of endpoints for a registry,
// or the full list of endpoints for a service across registries, since it limits
// scalability.
//
// Future optimizations will include grouping the endpoints by labels, gateway or region to
// reduce the time when subsetting or split-horizon is used. This design assumes pilot
// tracks all endpoints in the mesh and they fit in RAM - so limit is few M endpoints.
// It is possible to split the endpoint tracking in future.
type XDSUpdater interface {
	// EDSUpdate is called when the list of endpoints or labels in a Service is changed.
	// For each cluster and hostname, the full list of active endpoints (including empty list)
	// must be sent. The shard name is used as a key - current implementation is using the
	// registry name.
	EDSUpdate(shard ShardKey, hostname string, namespace string, entry []*IstioEndpoint)

	// EDSCacheUpdate is called when the list of endpoints or labels in a Service is changed.
	// For each cluster and hostname, the full list of active endpoints (including empty list)
	// must be sent. The shard name is used as a key - current implementation is using the
	// registry name.
	// Note: the difference with `EDSUpdate` is that it only update the cache rather than requesting a push
	EDSCacheUpdate(shard ShardKey, hostname string, namespace string, entry []*IstioEndpoint)

	// SvcUpdate is called when a service definition is updated/deleted.
	SvcUpdate(shard ShardKey, hostname string, namespace string, event Event)

	// ConfigUpdate is called to notify the XDS server of config updates and request a push.
	// The requests may be collapsed and throttled.
	ConfigUpdate(req *PushRequest)

	// ProxyUpdate is called to notify the XDS server to send a push to the specified proxy.
	// The requests may be collapsed and throttled.
	ProxyUpdate(clusterID cluster.ID, ip string)

	// RemoveShard removes all endpoints for the given shard key
	RemoveShard(shardKey ShardKey)
}

// PushRequest defines a request to push to proxies
// It is used to send updates to the config update debouncer and pass to the PushQueue.
type PushRequest struct {
	// Full determines whether a full push is required or not. If false, an incremental update will be sent.
	// Incremental pushes:
	// * Do not recompute the push context
	// * Do not recompute proxy state (such as ServiceInstances)
	// * Are not reported in standard metrics such as push time
	// As a result, configuration updates should never be incremental. Generally, only EDS will set this, but
	// in the future SDS will as well.
	Full bool

	// ConfigsUpdated keeps track of configs that have changed.
	// This is used as an optimization to avoid unnecessary pushes to proxies that are scoped with a Sidecar.
	// If this is empty, then all proxies will get an update.
	// Otherwise only proxies depend on these configs will get an update.
	// The kind of resources are defined in pkg/config/schemas.
	ConfigsUpdated sets.Set[ConfigKey]

	AddressesUpdated sets.Set[string]

	// Push stores the push context to use for the update. This may initially be nil, as we will
	// debounce changes before a PushContext is eventually created.
	Push *PushContext

	// Start represents the time a push was started. This represents the time of adding to the PushQueue.
	// Note that this does not include time spent debouncing.
	Start time.Time

	// Reason represents the reason for requesting a push. This should only be a fixed set of values,
	// to avoid unbounded cardinality in metrics. If this is not set, it may be automatically filled in later.
	// There should only be multiple reasons if the push request is the result of two distinct triggers, rather than
	// classifying a single trigger as having multiple reasons.
	Reason ReasonStats

	// Delta defines the resources that were added or removed as part of this push request.
	// This is set only on requests from the client which change the set of resources they (un)subscribe from.
	Delta ResourceDelta

	// Forced defines that configs should be generated and pushed regardless if they have changed or not.
	Forced bool
}

type ResourceDelta = xds.ResourceDelta

type ReasonStats map[TriggerReason]int

func NewReasonStats(reasons ...TriggerReason) ReasonStats {
	ret := make(ReasonStats)
	for _, reason := range reasons {
		ret.Add(reason)
	}
	return ret
}

func (r ReasonStats) Add(reason TriggerReason) {
	r[reason]++
}

func (r ReasonStats) Merge(other ReasonStats) {
	for reason, count := range other {
		r[reason] += count
	}
}

func (r ReasonStats) CopyMerge(other ReasonStats) ReasonStats {
	if len(r) == 0 {
		return other
	}
	if len(other) == 0 {
		return r
	}

	merged := make(ReasonStats, len(r)+len(other))
	merged.Merge(r)
	merged.Merge(other)

	return merged
}

func (r ReasonStats) Count() int {
	var ret int
	for _, count := range r {
		ret += count
	}
	return ret
}

func (r ReasonStats) Has(reason TriggerReason) bool {
	return r[reason] > 0
}

type TriggerReason string

// If adding a new reason, update xds/monitoring.go:triggerMetric
const (
	// EndpointUpdate describes a push triggered by an Endpoint change
	EndpointUpdate TriggerReason = "endpoint"
	// HeadlessEndpointUpdate describes a push triggered by an Endpoint change for headless service
	HeadlessEndpointUpdate TriggerReason = "headlessendpoint"
	// ConfigUpdate describes a push triggered by a config (generally and Istio CRD) change.
	ConfigUpdate TriggerReason = "config"
	// ServiceUpdate describes a push triggered by a Service change
	ServiceUpdate TriggerReason = "service"
	// ProxyUpdate describes a push triggered by a change to an individual proxy (such as label change)
	ProxyUpdate TriggerReason = "proxy"
	// GlobalUpdate describes a push triggered by a change to global config, such as mesh config
	GlobalUpdate TriggerReason = "global"
	// AmbientUpdate describes a push triggered by a change to ambient mesh config
	AmbientUpdate TriggerReason = "ambient"
	// UnknownTrigger describes a push triggered by an unknown reason
	UnknownTrigger TriggerReason = "unknown"
	// DebugTrigger describes a push triggered for debugging
	DebugTrigger TriggerReason = "debug"
	// SecretTrigger describes a push triggered for a Secret change
	SecretTrigger TriggerReason = "secret"
	// NetworksTrigger describes a push triggered for Networks change
	NetworksTrigger TriggerReason = "networks"
	// ProxyRequest describes a push triggered based on proxy request
	ProxyRequest TriggerReason = "proxyrequest"
	// DependentResource describes a push triggered based on a proxy request for a
	// resource that depends on this resource (e.g. a CDS request triggers an EDS response as well)
	// This is mainly used in Delta for now.
	DependentResource TriggerReason = "depdendentresource"
	// NamespaceUpdate describes a push triggered by a Namespace change
	NamespaceUpdate TriggerReason = "namespace"
	// ClusterUpdate describes a push triggered by a Cluster change
	ClusterUpdate TriggerReason = "cluster"
	// TagUpdate occurs when the revision's tags change, and all resources must be recalculated.
	TagUpdate TriggerReason = "tag"
)

// Merge two update requests together
// Merge behaves similarly to a list append; usage should in the form `a = a.merge(b)`.
// Importantly, Merge may decide to allocate a new PushRequest object or reuse the existing one - both
// inputs should not be used after completion.
func (pr *PushRequest) Merge(other *PushRequest) *PushRequest {
	if pr == nil {
		return other
	}
	if other == nil {
		return pr
	}

	// Keep the first (older) start time

	// Merge the two reasons. Note that we shouldn't deduplicate here, or we would under count
	if len(other.Reason) > 0 {
		if pr.Reason == nil {
			pr.Reason = make(map[TriggerReason]int)
		}
		pr.Reason.Merge(other.Reason)
	}

	// If either is full we need a full push
	pr.Full = pr.Full || other.Full

	// If either is forced we need a forced push
	pr.Forced = pr.Forced || other.Forced

	// The other push context is presumed to be later and more up to date
	if other.Push != nil {
		pr.Push = other.Push
	}

	if pr.ConfigsUpdated == nil {
		pr.ConfigsUpdated = other.ConfigsUpdated
	} else {
		pr.ConfigsUpdated.Merge(other.ConfigsUpdated)
	}

	if pr.AddressesUpdated == nil {
		pr.AddressesUpdated = other.AddressesUpdated
	} else {
		pr.AddressesUpdated.Merge(other.AddressesUpdated)
	}

	return pr
}

// CopyMerge two update requests together. Unlike Merge, this will not mutate either input.
// This should be used when we are modifying a shared PushRequest (typically any time it's in the context
// of a single proxy)
func (pr *PushRequest) CopyMerge(other *PushRequest) *PushRequest {
	if pr == nil {
		return other
	}
	if other == nil {
		return pr
	}

	var reason ReasonStats
	if len(pr.Reason)+len(other.Reason) > 0 {
		reason = make(ReasonStats)
		reason.Merge(pr.Reason)
		reason.Merge(other.Reason)
	}
	merged := &PushRequest{
		// Keep the first (older) start time
		Start: pr.Start,

		// If either is full we need a full push
		Full: pr.Full || other.Full,

		// If either is forced we need a forced push
		Forced: pr.Forced || other.Forced,

		// The other push context is presumed to be later and more up to date
		Push: other.Push,

		// Merge the two reasons. Note that we shouldn't deduplicate here, or we would under count
		Reason: reason,
	}

	if pr.ConfigsUpdated == nil && other.ConfigsUpdated == nil {
		merged.ConfigsUpdated = nil
	} else {
		merged.ConfigsUpdated = make(sets.Set[ConfigKey], len(pr.ConfigsUpdated)+len(other.ConfigsUpdated))
		merged.ConfigsUpdated.Merge(pr.ConfigsUpdated)
		merged.ConfigsUpdated.Merge(other.ConfigsUpdated)
	}

	if len(pr.AddressesUpdated) > 0 || len(other.AddressesUpdated) > 0 {
		merged.AddressesUpdated = make(sets.Set[string], len(pr.AddressesUpdated)+len(other.AddressesUpdated))
		merged.AddressesUpdated.Merge(pr.AddressesUpdated)
		merged.AddressesUpdated.Merge(other.AddressesUpdated)
	}

	return merged
}

func (pr *PushRequest) IsRequest() bool {
	return len(pr.Reason) == 1 && pr.Reason.Has(ProxyRequest)
}

func (pr *PushRequest) IsProxyUpdate() bool {
	return pr.Reason.Has(ProxyUpdate)
}

func (pr *PushRequest) PushReason() string {
	if pr.IsRequest() {
		return " request"
	}
	return ""
}

// ProxyPushStatus represents an event captured during config push to proxies.
// It may contain additional message and the affected proxy.
type ProxyPushStatus struct {
	Proxy   string `json:"proxy,omitempty"`
	Message string `json:"message,omitempty"`
}

// AddMetric will add an case to the metric.
func (ps *PushContext) AddMetric(metric monitoring.Metric, key string, proxyID, msg string) {
	if ps == nil {
		log.Infof("Metric without context %s %v %s", key, proxyID, msg)
		return
	}
	ps.proxyStatusMutex.Lock()
	defer ps.proxyStatusMutex.Unlock()

	metricMap, f := ps.ProxyStatus[metric.Name()]
	if !f {
		metricMap = map[string]ProxyPushStatus{}
		ps.ProxyStatus[metric.Name()] = metricMap
	}
	ev := ProxyPushStatus{Message: msg, Proxy: proxyID}
	metricMap[key] = ev
}

func (ps *PushContext) GetMetric(metric string) map[string]ProxyPushStatus {
	ps.proxyStatusMutex.RLock()
	defer ps.proxyStatusMutex.RUnlock()
	return ps.ProxyStatus[metric]
}

var (

	// EndpointNoPod tracks endpoints without an associated pod. This is an error condition, since
	// we can't figure out the labels. It may be a transient problem, if endpoint is processed before
	// pod.
	EndpointNoPod = monitoring.NewGauge(
		"endpoint_no_pod",
		"Endpoints without an associated pod.",
	)

	// ProxyStatusNoService represents proxies not selected by any service
	// This can be normal - for workloads that act only as client, or are not covered by a Service.
	// It can also be an error, for example in cases the Endpoint list of a service was not updated by the time
	// the sidecar calls.
	// Updated by GetProxyServiceTargets
	ProxyStatusNoService = monitoring.NewGauge(
		"pilot_no_ip",
		"Pods not found in the endpoint table, possibly invalid.",
	)

	// ProxyStatusEndpointNotReady represents proxies found not be ready.
	// Updated by GetProxyServiceTargets. Normal condition when starting
	// an app with readiness, error if it doesn't change to 0.
	ProxyStatusEndpointNotReady = monitoring.NewGauge(
		"pilot_endpoint_not_ready",
		"Endpoint found in unready state.",
	)

	// ProxyStatusConflictOutboundListenerTCPOverTCP metric tracks number of
	// TCP listeners that conflicted with existing TCP listeners on same port
	ProxyStatusConflictOutboundListenerTCPOverTCP = monitoring.NewGauge(
		"pilot_conflict_outbound_listener_tcp_over_current_tcp",
		"Number of conflicting tcp listeners with current tcp listener.",
	)

	// ProxyStatusConflictInboundListener tracks cases of multiple inbound
	// listeners - 2 services selecting the same port of the pod.
	ProxyStatusConflictInboundListener = monitoring.NewGauge(
		"pilot_conflict_inbound_listener",
		"Number of conflicting inbound listeners.",
	)

	// DuplicatedClusters tracks duplicate clusters seen while computing CDS
	DuplicatedClusters = monitoring.NewGauge(
		"pilot_duplicate_envoy_clusters",
		"Duplicate envoy clusters caused by service entries with same hostname",
	)

	// DNSNoEndpointClusters tracks dns clusters without endpoints
	DNSNoEndpointClusters = monitoring.NewGauge(
		"pilot_dns_cluster_without_endpoints",
		"DNS clusters without endpoints caused by the endpoint field in "+
			"STRICT_DNS type cluster is not set or the corresponding subset cannot select any endpoint",
	)

	// ProxyStatusClusterNoInstances tracks clusters (services) without workloads.
	ProxyStatusClusterNoInstances = monitoring.NewGauge(
		"pilot_eds_no_instances",
		"Number of clusters without instances.",
	)

	// DuplicatedDomains tracks rejected VirtualServices due to duplicated hostname.
	DuplicatedDomains = monitoring.NewGauge(
		"pilot_vservice_dup_domain",
		"Virtual services with dup domains.",
	)

	// DuplicatedSubsets tracks duplicate subsets that we rejected while merging multiple destination rules for same host
	DuplicatedSubsets = monitoring.NewGauge(
		"pilot_destrule_subsets",
		"Duplicate subsets across destination rules for same host",
	)

	// totalVirtualServices tracks the total number of virtual service
	totalVirtualServices = monitoring.NewGauge(
		"pilot_virt_services",
		"Total virtual services known to pilot.",
	)

	// LastPushStatus preserves the metrics and data collected during lasts global push.
	// It can be used by debugging tools to inspect the push event. It will be reset after each push with the
	// new version.
	LastPushStatus *PushContext
	// LastPushMutex will protect the LastPushStatus
	LastPushMutex sync.Mutex

	// All metrics we registered.
	metrics = []monitoring.Metric{
		DNSNoEndpointClusters,
		EndpointNoPod,
		ProxyStatusNoService,
		ProxyStatusEndpointNotReady,
		ProxyStatusConflictOutboundListenerTCPOverTCP,
		ProxyStatusConflictInboundListener,
		DuplicatedClusters,
		ProxyStatusClusterNoInstances,
		DuplicatedDomains,
		DuplicatedSubsets,
	}
)

// NewPushContext creates a new PushContext structure to track push status.
func NewPushContext() *PushContext {
	return &PushContext{
		ServiceIndex:            newServiceIndex(),
		virtualServiceIndex:     newVirtualServiceIndex(),
		destinationRuleIndex:    newDestinationRuleIndex(),
		sidecarIndex:            newSidecarIndex(),
		envoyFiltersByNamespace: map[string][]*EnvoyFilterWrapper{},
		gatewayIndex:            newGatewayIndex(),
		ProxyStatus:             map[string]map[string]ProxyPushStatus{},
		serviceAccounts:         map[serviceAccountKey][]string{},
	}
}

// AddPublicServices adds the services to context public services - mainly used in tests.
func (ps *PushContext) AddPublicServices(services []*Service) {
	ps.ServiceIndex.public = append(ps.ServiceIndex.public, services...)
}

// AddServiceInstances adds instances to the context service instances - mainly used in tests.
func (ps *PushContext) AddServiceInstances(service *Service, instances map[int][]*IstioEndpoint) {
	svcKey := service.Key()
	for port, inst := range instances {
		if _, exists := ps.ServiceIndex.instancesByPort[svcKey]; !exists {
			ps.ServiceIndex.instancesByPort[svcKey] = make(map[int][]*IstioEndpoint)
		}
		ps.ServiceIndex.instancesByPort[svcKey][port] = append(ps.ServiceIndex.instancesByPort[svcKey][port], inst...)
	}
}

// StatusJSON implements json.Marshaller, with a lock.
func (ps *PushContext) StatusJSON() ([]byte, error) {
	if ps == nil {
		return []byte{'{', '}'}, nil
	}
	ps.proxyStatusMutex.RLock()
	defer ps.proxyStatusMutex.RUnlock()
	return json.MarshalIndent(ps.ProxyStatus, "", "    ")
}

// OnConfigChange is called when a config change is detected.
func (ps *PushContext) OnConfigChange() {
	LastPushMutex.Lock()
	LastPushStatus = ps
	LastPushMutex.Unlock()
	ps.UpdateMetrics()
}

// UpdateMetrics will update the prometheus metrics based on the
// current status of the push.
func (ps *PushContext) UpdateMetrics() {
	ps.proxyStatusMutex.RLock()
	defer ps.proxyStatusMutex.RUnlock()

	for _, pm := range metrics {
		mmap := ps.ProxyStatus[pm.Name()]
		pm.Record(float64(len(mmap)))
	}
}

// It is called after virtual service short host name is resolved to FQDN
func virtualServiceDestinations(v *networking.VirtualService) map[string]sets.Set[int] {
	return virtualServiceDestinationsFilteredBySourceNamespace(v, "")
}

// It is called after virtual service short host name is resolved to FQDN
// It filters destinations present in VirtualService by using configNamespace, when the value is empty string, then filtering is disabled
func virtualServiceDestinationsFilteredBySourceNamespace(v *networking.VirtualService, configNamespace string) map[string]sets.Set[int] {
	if v == nil {
		return nil
	}

	out := make(map[string]sets.Set[int])

	addDestination := func(host string, port *networking.PortSelector) {
		// Use the value 0 as a sentinel indicating that one of the destinations
		// in the Virtual Service does not specify a port for this host.
		pn := 0
		if port != nil {
			pn = int(port.Number)
		}
		sets.InsertOrNew(out, host, pn)
	}

	for _, h := range v.Http {
		if configNamespace != "" {
			namespaceMatched := true
			for _, m := range h.Match {
				if m.SourceNamespace != "" {
					if m.SourceNamespace == configNamespace {
						namespaceMatched = true
						break
					}
					namespaceMatched = false
				}
			}
			if !namespaceMatched {
				continue
			}
		}
		for _, r := range h.Route {
			if r.Destination != nil {
				addDestination(r.Destination.Host, r.Destination.GetPort())
			}
		}
		if h.Mirror != nil {
			addDestination(h.Mirror.Host, h.Mirror.GetPort())
		}
		for _, m := range h.Mirrors {
			if m.Destination != nil {
				addDestination(m.Destination.Host, m.Destination.GetPort())
			}
		}
	}
	for _, t := range v.Tcp {
		for _, r := range t.Route {
			if r.Destination != nil {
				addDestination(r.Destination.Host, r.Destination.GetPort())
			}
		}
	}
	for _, t := range v.Tls {
		for _, r := range t.Route {
			if r.Destination != nil {
				addDestination(r.Destination.Host, r.Destination.GetPort())
			}
		}
	}

	return out
}

func (ps *PushContext) ExtraWaypointServices(proxy *Proxy, patches *MergedEnvoyFilterWrapper) (sets.Set[NamespacedHostname], sets.String) {
	return ps.extraServicesForProxy(proxy, patches)
}

// GatewayServices returns the set of services which are referred from the proxy gateways.
func (ps *PushContext) GatewayServices(proxy *Proxy, patches *MergedEnvoyFilterWrapper) []*Service {
	svcs := proxy.SidecarScope.services

	// host set.
	namespacedHostsFromGateways, hostsFromGateways := ps.extraServicesForProxy(proxy, patches)
	// MergedGateway will be nil when there are no configs in the
	// system during initial installation.
	if proxy.MergedGateway != nil {
		for _, gw := range proxy.MergedGateway.GatewayNameForServer {
			hostsFromGateways.Merge(ps.virtualServiceIndex.destinationsByGateway[gw])
		}
	}
	log.Debugf("GatewayServices: gateway %v is exposing these hosts:%v", proxy.ID, hostsFromGateways)

	gwSvcs := make([]*Service, 0, len(svcs))

	for _, s := range svcs {
		svcHost := string(s.Hostname)
		if hostsFromGateways.Contains(svcHost) || namespacedHostsFromGateways.Contains(NamespacedHostname{
			Hostname:  s.Hostname,
			Namespace: s.Attributes.Namespace,
		}) {
			gwSvcs = append(gwSvcs, s)
		}
	}

	log.Debugf("GatewayServices: gateways len(services)=%d, len(filtered)=%d", len(svcs), len(gwSvcs))

	return gwSvcs
}

func (ps *PushContext) ServicesAttachedToMesh() map[string]sets.String {
	return ps.virtualServiceIndex.referencedDestinations
}

func (ps *PushContext) ServiceAttachedToGateway(hostname string, namespace string, proxy *Proxy) bool {
	gw := proxy.MergedGateway
	// MergedGateway will be nil when there are no configs in the
	// system during initial installation.
	if gw == nil {
		return false
	}
	if gw.ContainsAutoPassthroughGateways {
		return true
	}
	for _, g := range gw.GatewayNameForServer {
		if hosts := ps.virtualServiceIndex.destinationsByGateway[g]; hosts != nil {
			if hosts.Contains(hostname) {
				return true
			}
		}
	}
	patches := ps.EnvoyFilters(proxy)
	namespaced, hosts := ps.extraServicesForProxy(proxy, patches)
	return hosts.Contains(hostname) || namespaced.Contains(NamespacedHostname{Hostname: host.Name(hostname), Namespace: namespace})
}

// wellknownProviders is a list of all known providers.
// This exists
var wellknownProviders = sets.New(
	"envoy_ext_authz_http",
	"envoy_ext_authz_grpc",
	"zipkin",
	"lightstep",
	"datadog",
	"opencensus",
	"stackdriver",
	"prometheus",
	"skywalking",
	"envoy_http_als",
	"envoy_tcp_als",
	"envoy_otel_als",
	"opentelemetry",
	"envoy_file_access_log",
)

func AssertProvidersHandled(expected int) {
	if expected != len(wellknownProviders) {
		panic(fmt.Sprintf("Not all providers handled; This function handles %v but there are %v known providers", expected, len(wellknownProviders)))
	}
}

// addHostsFromMeshConfigProvidersHandled contains the number of providers we handle below.
// This is to ensure this stays in sync as new handlers are added
// STOP. DO NOT UPDATE THIS WITHOUT UPDATING extraServicesForProxy.
const addHostsFromMeshConfigProvidersHandled = 14

// extraServicesForProxy returns a subset of services referred from the proxy gateways, including:
// 1. MeshConfig.ExtensionProviders
// 2. RequestAuthentication.JwtRules.JwksUri
// 3. EnvoyFilters with explicitly annotated references
func (ps *PushContext) extraServicesForProxy(proxy *Proxy, patches *MergedEnvoyFilterWrapper) (sets.Set[NamespacedHostname], sets.String) {
	hosts := sets.String{}
	namespaceScoped := sets.New[NamespacedHostname]()
	addService := func(s string) {
		if ns, h, ok := strings.Cut(s, "/"); ok {
			namespaceScoped.Insert(NamespacedHostname{
				Hostname:  host.Name(h),
				Namespace: ns,
			})
		} else {
			hosts.Insert(s)
		}
	}
	// add services from MeshConfig.ExtensionProviders
	for _, prov := range ps.Mesh.ExtensionProviders {
		switch p := prov.Provider.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp:
			addService(p.EnvoyExtAuthzHttp.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc:
			addService(p.EnvoyExtAuthzGrpc.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_Zipkin:
			addService(p.Zipkin.Service)
		//nolint: staticcheck  // Lightstep deprecated
		case *meshconfig.MeshConfig_ExtensionProvider_Lightstep:
			log.Warnf("Lightstep provider is deprecated, please use OpenTelemetry instead")
			addService(p.Lightstep.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_Datadog:
			addService(p.Datadog.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_Skywalking:
			addService(p.Skywalking.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_Opencensus:
			log.Warnf("Opencensus provider is deprecated, please use OpenTelemetry instead")
			//nolint: staticcheck
			addService(p.Opencensus.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_Opentelemetry:
			addService(p.Opentelemetry.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls:
			addService(p.EnvoyHttpAls.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls:
			addService(p.EnvoyTcpAls.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls:
			addService(p.EnvoyOtelAls.Service)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog: // No services
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus: // No services
		case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver: // No services
		}
	}
	// add services from RequestAuthentication.JwtRules.JwksUri
	if features.JwksFetchMode != jwt.Istiod {
		forWorkload := PolicyMatcherForProxy(proxy)
		jwtPolicies := ps.AuthnPolicies.GetJwtPoliciesForWorkload(forWorkload)
		for _, cfg := range jwtPolicies {
			rules := cfg.Spec.(*v1beta1.RequestAuthentication).JwtRules
			for _, r := range rules {
				if uri := r.GetJwksUri(); len(uri) > 0 {
					jwksInfo, err := security.ParseJwksURI(uri)
					if err == nil {
						hosts.Insert(jwksInfo.Hostname.String())
					}
				}
			}
		}
	}

	if patches != nil {
		namespaceScoped.Merge(patches.ReferencedNamespacedServices)
		hosts.Merge(patches.ReferencedServices)
	}
	return namespaceScoped, hosts
}

// servicesExportedToNamespace returns the list of services that are visible to a namespace.
// namespace "" indicates all namespaces
func (ps *PushContext) servicesExportedToNamespace(ns string) []*Service {
	var out []*Service

	// First add private services and explicitly exportedTo services
	if ns == NamespaceAll {
		out = make([]*Service, 0, len(ps.ServiceIndex.privateByNamespace)+len(ps.ServiceIndex.public))
		for _, privateServices := range ps.ServiceIndex.privateByNamespace {
			out = append(out, privateServices...)
		}
	} else {
		out = make([]*Service, 0, len(ps.ServiceIndex.privateByNamespace[ns])+
			len(ps.ServiceIndex.exportedToNamespace[ns])+len(ps.ServiceIndex.public))
		out = append(out, ps.ServiceIndex.privateByNamespace[ns]...)
		out = append(out, ps.ServiceIndex.exportedToNamespace[ns]...)
	}

	// Second add public services
	out = append(out, ps.ServiceIndex.public...)

	return out
}

// GetAllServices returns the total services within the mesh.
// Note: per proxy services should use SidecarScope.Services.
func (ps *PushContext) GetAllServices() []*Service {
	return ps.servicesExportedToNamespace(NamespaceAll)
}

// ServiceForHostname returns the service associated with a given hostname following SidecarScope
func (ps *PushContext) ServiceForHostname(proxy *Proxy, hostname host.Name) *Service {
	if proxy != nil && proxy.SidecarScope != nil {
		return proxy.SidecarScope.servicesByHostname[hostname]
	}

	// SidecarScope shouldn't be null here. If it is, we can't disambiguate the hostname to use for a namespace,
	// so the selection must be undefined.
	for _, service := range ps.ServiceIndex.HostnameAndNamespace[hostname] {
		return service
	}

	// No service found
	return nil
}

// IsServiceVisible returns true if the input service is visible to the given namespace.
func (ps *PushContext) IsServiceVisible(service *Service, namespace string) bool {
	if service == nil {
		return false
	}

	ns := service.Attributes.Namespace
	if service.Attributes.ExportTo.IsEmpty() {
		if ps.exportToDefaults.service.Contains(visibility.Private) {
			return ns == namespace
		} else if ps.exportToDefaults.service.Contains(visibility.Public) {
			return true
		}
	}

	return service.Attributes.ExportTo.Contains(visibility.Public) ||
		(service.Attributes.ExportTo.Contains(visibility.Private) && ns == namespace) ||
		service.Attributes.ExportTo.Contains(visibility.Instance(namespace))
}

// VirtualServicesForGateway lists all virtual services bound to the specified gateways
// This replaces store.VirtualServices. Used only by the gateways
// Sidecars use the egressListener.VirtualServices().
//
// Note that for generating the imported virtual services of sidecar egress
// listener, we don't call this function to copy configs for performance issues.
// Instead, we pass the virtualServiceIndex directly into SelectVirtualServices
// function.
func (ps *PushContext) VirtualServicesForGateway(proxyNamespace, gateway string) []config.Config {
	name := types.NamespacedName{
		Namespace: proxyNamespace,
		Name:      gateway,
	}
	res := make([]config.Config, 0, len(ps.virtualServiceIndex.privateByNamespaceAndGateway[name])+
		len(ps.virtualServiceIndex.exportedToNamespaceByGateway[name])+
		len(ps.virtualServiceIndex.publicByGateway[gateway]))
	res = append(res, ps.virtualServiceIndex.privateByNamespaceAndGateway[name]...)
	res = append(res, ps.virtualServiceIndex.exportedToNamespaceByGateway[name]...)
	// Favor same-namespace Gateway routes, to give the "consumer override" preference.
	// We do 2 iterations here to avoid extra allocations.
	for _, vs := range ps.virtualServiceIndex.publicByGateway[gateway] {
		if UseGatewaySemantics(vs) && vs.Namespace == proxyNamespace {
			res = append(res, vs)
		}
	}
	for _, vs := range ps.virtualServiceIndex.publicByGateway[gateway] {
		if !(UseGatewaySemantics(vs) && vs.Namespace == proxyNamespace) {
			res = append(res, vs)
		}
	}

	return res
}

// DelegateVirtualServices lists all the delegate virtual services configkeys associated with the provided virtual services
func (ps *PushContext) DelegateVirtualServices(vses []config.Config) []ConfigHash {
	var out []ConfigHash
	for _, vs := range vses {
		for _, delegate := range ps.virtualServiceIndex.delegates[ConfigKey{Kind: kind.VirtualService, Namespace: vs.Namespace, Name: vs.Name}] {
			out = append(out, delegate.HashCode())
		}
	}
	return out
}

// getSidecarScope returns a SidecarScope object associated with the
// proxy. The SidecarScope object is a semi-processed view of the service
// registry, and config state associated with the sidecar crd. The scope contains
// a set of inbound and outbound listeners, services/configs per listener,
// etc. The sidecar scopes are precomputed in the initSidecarContext
// function based on the Sidecar API objects in each namespace. If there is
// no sidecar api object, a default sidecarscope is assigned to the
// namespace which enables connectivity to all services in the mesh.
//
// Callers can check if the sidecarScope is from user generated object or not
// by checking the sidecarScope.Config field, that contains the user provided config
func (ps *PushContext) getSidecarScope(proxy *Proxy, workloadLabels labels.Instance) *SidecarScope {
	// TODO: logic to merge multiple sidecar resources
	// Currently we assume that there will be only one sidecar config for a namespace.
	sidecars, hasSidecar := ps.sidecarIndex.sidecarsByNamespace[proxy.ConfigNamespace]
	switch proxy.Type {
	case Router, Waypoint:
		ps.sidecarIndex.derivedSidecarMutex.Lock()
		defer ps.sidecarIndex.derivedSidecarMutex.Unlock()

		// Gateways always use default sidecar scope.
		if sc, f := ps.sidecarIndex.defaultSidecarsByNamespace[proxy.ConfigNamespace]; f {
			return sc
		}

		if sc, f := ps.sidecarIndex.sidecarsForGatewayByNamespace[proxy.ConfigNamespace]; f {
			return sc
		}

		// We need to compute this namespace
		computed := DefaultSidecarScopeForGateway(ps, proxy.ConfigNamespace)
		ps.sidecarIndex.sidecarsForGatewayByNamespace[proxy.ConfigNamespace] = computed
		return computed
	case SidecarProxy:
		if hasSidecar {
			for _, wrapper := range sidecars {
				if wrapper.Sidecar != nil {
					sidecar := wrapper.Sidecar
					// if there is no workload selector, the config applies to all workloads
					// if there is a workload selector, check for matching workload labels
					if sidecar.GetWorkloadSelector() != nil {
						workloadSelector := labels.Instance(sidecar.GetWorkloadSelector().GetLabels())
						// exclude workload selector that not match
						if !workloadSelector.SubsetOf(workloadLabels) {
							continue
						}
					}

					// it is guaranteed sidecars with selectors are put in front
					// and the sidecars are sorted by creation timestamp,
					// return exact/wildcard matching one directly
					return wrapper
				}
				// this happens at last, it is the default sidecar scope
				return wrapper
			}
		}
		ps.sidecarIndex.derivedSidecarMutex.Lock()
		defer ps.sidecarIndex.derivedSidecarMutex.Unlock()

		if ps.sidecarIndex.meshRootSidecarConfig != nil {
			if sc, exists := ps.sidecarIndex.meshRootSidecarsByNamespace[proxy.ConfigNamespace]; exists {
				// We have already computed the scope for this namespace, just return it.
				return sc
			}
			// We need to compute this namespace
			computed := convertToSidecarScope(ps, ps.sidecarIndex.meshRootSidecarConfig, proxy.ConfigNamespace)
			ps.sidecarIndex.meshRootSidecarsByNamespace[proxy.ConfigNamespace] = computed
			return computed
		}
		if sc, exists := ps.sidecarIndex.defaultSidecarsByNamespace[proxy.ConfigNamespace]; exists {
			// We have already computed the scope for this namespace, just return it.
			return sc
		}
		// We need to compute this namespace
		computed := convertToSidecarScope(ps, ps.sidecarIndex.meshRootSidecarConfig, proxy.ConfigNamespace)
		ps.sidecarIndex.defaultSidecarsByNamespace[proxy.ConfigNamespace] = computed
		return computed
	}
	return nil
}

// destinationRule returns a destination rule for a service name in a given namespace.
func (ps *PushContext) destinationRule(proxyNameSpace string, service *Service) []*ConsolidatedDestRule {
	if service == nil {
		return nil
	}
	// If the proxy config namespace is same as the root config namespace
	// look for dest rules in the service's namespace first. This hack is needed
	// because sometimes, istio-system tends to become the root config namespace.
	// Destination rules are defined here for global purposes. We do not want these
	// catch all destination rules to be the only dest rule, when processing CDS for
	// proxies like the istio-ingressgateway or istio-egressgateway.
	// If there are no service specific dest rules, we will end up picking up the same
	// rules anyway, later in the code

	// 1. select destination rule from proxy config namespace
	if proxyNameSpace != ps.Mesh.RootNamespace {
		// search through the DestinationRules in proxy's namespace first
		if ps.destinationRuleIndex.namespaceLocal[proxyNameSpace] != nil {
			if _, drs, ok := MostSpecificHostMatch(service.Hostname,
				ps.destinationRuleIndex.namespaceLocal[proxyNameSpace].specificDestRules,
				ps.destinationRuleIndex.namespaceLocal[proxyNameSpace].wildcardDestRules,
			); ok {
				return drs
			}
		}
	} else {
		// If this is a namespace local DR in the same namespace, this must be meant for this proxy, so we do not
		// need to worry about overriding other DRs with *.local type rules here. If we ignore this, then exportTo=. in
		// root namespace would always be ignored
		if _, drs, ok := MostSpecificHostMatch(service.Hostname,
			ps.destinationRuleIndex.rootNamespaceLocal.specificDestRules,
			ps.destinationRuleIndex.rootNamespaceLocal.wildcardDestRules,
		); ok {
			return drs
		}
	}

	// 2. select destination rule from service namespace
	svcNs := service.Attributes.Namespace

	// This can happen when finding the subset labels for a proxy in root namespace.
	// Because based on a pure cluster's fqdn, we do not know the service and
	// construct a fake service without setting Attributes at all.
	if svcNs == "" {
		for _, svc := range ps.servicesExportedToNamespace(proxyNameSpace) {
			if service.Hostname == svc.Hostname && svc.Attributes.Namespace != "" {
				svcNs = svc.Attributes.Namespace
				break
			}
		}
	}

	// 3. if no private/public rule matched in the calling proxy's namespace,
	// check the target service's namespace for exported rules
	if svcNs != "" {
		if out := ps.getExportedDestinationRuleFromNamespace(svcNs, service.Hostname, proxyNameSpace); len(out) > 0 {
			return out
		}
	}

	// 4. if no public/private rule in calling proxy's namespace matched, and no public rule in the
	// target service's namespace matched, search for any exported destination rule in the config root namespace
	if out := ps.getExportedDestinationRuleFromNamespace(ps.Mesh.RootNamespace, service.Hostname, proxyNameSpace); len(out) > 0 {
		return out
	}

	return nil
}

func (ps *PushContext) getExportedDestinationRuleFromNamespace(owningNamespace string, hostname host.Name, clientNamespace string) []*ConsolidatedDestRule {
	if ps.destinationRuleIndex.exportedByNamespace[owningNamespace] != nil {
		if _, drs, ok := MostSpecificHostMatch(hostname,
			ps.destinationRuleIndex.exportedByNamespace[owningNamespace].specificDestRules,
			ps.destinationRuleIndex.exportedByNamespace[owningNamespace].wildcardDestRules,
		); ok {
			out := make([]*ConsolidatedDestRule, 0, len(drs))
			for _, mdr := range drs {
				// Check if the dest rule for this host is actually exported to the proxy's (client) namespace
				exportToSet := mdr.exportTo
				if exportToSet.IsEmpty() || exportToSet.Contains(visibility.Public) || exportToSet.Contains(visibility.Instance(clientNamespace)) {
					out = append(out, mdr)
				}
			}
			return out
		}
	}
	return nil
}

// IsClusterLocal indicates whether the endpoints for the service should only be accessible to clients
// within the cluster.
func (ps *PushContext) IsClusterLocal(service *Service) bool {
	if ps == nil || service == nil {
		return false
	}
	return ps.clusterLocalHosts.IsClusterLocal(service.Hostname)
}

// InitContext will initialize the data structures used for code generation.
// This should be called before starting the push, from the thread creating
// the push context.
func (ps *PushContext) InitContext(env *Environment, oldPushContext *PushContext, pushReq *PushRequest) {
	// Acquire a lock to ensure we don't concurrently initialize the same PushContext.
	// If this does happen, one thread will block then exit early from InitDone=true
	ps.initializeMutex.Lock()
	defer ps.initializeMutex.Unlock()
	if ps.InitDone.Load() {
		return
	}

	ps.Mesh = env.Mesh()
	ps.Networks = env.MeshNetworks()

	// Must be initialized first as initServiceRegistry/VirtualServices/Destrules
	// use the default export map.
	ps.initDefaultExportMaps()

	// create new or incremental update
	if pushReq == nil || oldPushContext == nil || !oldPushContext.InitDone.Load() || pushReq.Forced {
		ps.createNewContext(env)
	} else {
		ps.updateContext(env, oldPushContext, pushReq)
	}

	ps.networkMgr = env.NetworkManager

	ps.clusterLocalHosts = env.ClusterLocal().GetClusterLocalHosts()

	ps.InitDone.Store(true)
}

func (ps *PushContext) createNewContext(env *Environment) {
	ps.initServiceRegistry(env, nil)

	ps.initKubernetesGateways(env)

	ps.initVirtualServices(env)

	ps.initDestinationRules(env)
	ps.initAuthnPolicies(env)

	ps.initAuthorizationPolicies(env)
	ps.initTelemetry(env)
	ps.initProxyConfigs(env)
	ps.initWasmPlugins(env)
	ps.initEnvoyFilters(env, nil, nil)
	ps.initGateways(env)
	ps.initAmbient(env)

	// Must be initialized in the end
	ps.initSidecarScopes(env)
}

func (ps *PushContext) updateContext(
	env *Environment,
	oldPushContext *PushContext,
	pushReq *PushRequest,
) {
	var servicesChanged, virtualServicesChanged, destinationRulesChanged, gatewayChanged,
		authnChanged, authzChanged, envoyFiltersChanged, sidecarsChanged, telemetryChanged,
		wasmPluginsChanged, proxyConfigsChanged bool

	changedEnvoyFilters := sets.New[ConfigKey]()

	// We do not need to watch Ingress or Gateway API changes. Both of these have their own controllers which will send
	// events for Istio types (Gateway and VirtualService).
	for conf := range pushReq.ConfigsUpdated {
		switch conf.Kind {
		case kind.ServiceEntry, kind.DNSName:
			servicesChanged = true
		case kind.DestinationRule:
			destinationRulesChanged = true
		case kind.VirtualService:
			virtualServicesChanged = true
		case kind.Gateway:
			gatewayChanged = true
		case kind.Sidecar:
			sidecarsChanged = true
		case kind.WasmPlugin:
			wasmPluginsChanged = true
		case kind.EnvoyFilter:
			envoyFiltersChanged = true
			changedEnvoyFilters.Insert(conf)
		case kind.AuthorizationPolicy:
			authzChanged = true
		case kind.RequestAuthentication,
			kind.PeerAuthentication:
			authnChanged = true
		case kind.Telemetry:
			telemetryChanged = true
		case kind.ProxyConfig:
			proxyConfigsChanged = true
		}
	}

	if servicesChanged {
		// Services have changed. initialize service registry
		ps.initServiceRegistry(env, pushReq.ConfigsUpdated)
	} else {
		// make sure we copy over things that would be generated in initServiceRegistry
		ps.ServiceIndex = oldPushContext.ServiceIndex
		ps.serviceAccounts = oldPushContext.serviceAccounts
	}

	if servicesChanged {
		// Gateway status depends on services, so recompute if they change as well
		ps.initKubernetesGateways(env)
	} else {
		ps.GatewayAPIController = oldPushContext.GatewayAPIController
	}

	if virtualServicesChanged {
		ps.initVirtualServices(env)
	} else {
		ps.virtualServiceIndex = oldPushContext.virtualServiceIndex
	}

	if destinationRulesChanged {
		ps.initDestinationRules(env)
	} else {
		ps.destinationRuleIndex = oldPushContext.destinationRuleIndex
	}

	if authnChanged {
		ps.initAuthnPolicies(env)
	} else {
		ps.AuthnPolicies = oldPushContext.AuthnPolicies
	}

	if authzChanged {
		ps.initAuthorizationPolicies(env)
	} else {
		ps.AuthzPolicies = oldPushContext.AuthzPolicies
	}

	if telemetryChanged {
		ps.initTelemetry(env)
	} else {
		ps.Telemetry = oldPushContext.Telemetry
	}

	if proxyConfigsChanged {
		ps.initProxyConfigs(env)
	} else {
		ps.ProxyConfigs = oldPushContext.ProxyConfigs
	}

	if wasmPluginsChanged {
		ps.initWasmPlugins(env)
	} else {
		ps.wasmPluginsByNamespace = oldPushContext.wasmPluginsByNamespace
	}

	if envoyFiltersChanged {
		ps.initEnvoyFilters(env, changedEnvoyFilters, oldPushContext.envoyFiltersByNamespace)
	} else {
		ps.envoyFiltersByNamespace = oldPushContext.envoyFiltersByNamespace
	}

	if gatewayChanged {
		ps.initGateways(env)
	} else {
		ps.gatewayIndex = oldPushContext.gatewayIndex
	}

	ps.initAmbient(env)

	// Must be initialized in the end
	// Sidecars need to be updated if services, virtual services, destination rules, or the sidecar configs change
	if servicesChanged || virtualServicesChanged || destinationRulesChanged || sidecarsChanged {
		ps.initSidecarScopes(env)
	} else {
		// new ADS connection may insert new entry to computedSidecarsByNamespace/gatewayDefaultSidecarsByNamespace.
		oldPushContext.sidecarIndex.derivedSidecarMutex.RLock()
		ps.sidecarIndex = oldPushContext.sidecarIndex
		oldPushContext.sidecarIndex.derivedSidecarMutex.RUnlock()
	}
}

// Caches list of services in the registry, and creates a map
// of hostname to service
func (ps *PushContext) initServiceRegistry(env *Environment, configsUpdate sets.Set[ConfigKey]) {
	// Sort the services in order of creation.
	allServices := SortServicesByCreationTime(env.Services())
	resolveServiceAliases(allServices, configsUpdate)

	for _, s := range allServices {
		portMap := map[string]int{}
		ports := sets.New[int]()
		for _, port := range s.Ports {
			portMap[port.Name] = port.Port
			ports.Insert(port.Port)
		}

		svcKey := s.Key()
		if _, ok := ps.ServiceIndex.instancesByPort[svcKey]; !ok {
			ps.ServiceIndex.instancesByPort[svcKey] = make(map[int][]*IstioEndpoint)
		}
		shards, ok := env.EndpointIndex.ShardsForService(string(s.Hostname), s.Attributes.Namespace)
		if ok {
			instancesByPort := shards.CopyEndpoints(portMap, ports)
			// Iterate over the instances and add them to the service index to avoid overiding the existing port instances.
			for port, instances := range instancesByPort {
				ps.ServiceIndex.instancesByPort[svcKey][port] = instances
			}
		}
		if _, f := ps.ServiceIndex.HostnameAndNamespace[s.Hostname]; !f {
			ps.ServiceIndex.HostnameAndNamespace[s.Hostname] = map[string]*Service{}
		}
		// In some scenarios, there may be multiple Services defined for the same hostname due to ServiceEntry allowing
		// arbitrary hostnames. In these cases, we want to pick the first Service, which is the oldest. This ensures
		// newly created Services cannot take ownership unexpectedly.
		// However, the Service is from Kubernetes it should take precedence over ones not. This prevents someone from
		// "domain squatting" on the hostname before a Kubernetes Service is created.
		if existing := ps.ServiceIndex.HostnameAndNamespace[s.Hostname][s.Attributes.Namespace]; existing != nil &&
			!(existing.Attributes.ServiceRegistry != provider.Kubernetes && s.Attributes.ServiceRegistry == provider.Kubernetes) {
			log.Debugf("Service %s/%s from registry %s ignored by %s/%s/%s", s.Attributes.Namespace, s.Hostname, s.Attributes.ServiceRegistry,
				existing.Attributes.ServiceRegistry, existing.Attributes.Namespace, existing.Hostname)
		} else {
			ps.ServiceIndex.HostnameAndNamespace[s.Hostname][s.Attributes.Namespace] = s
		}

		ns := s.Attributes.Namespace
		if s.Attributes.ExportTo.IsEmpty() {
			if ps.exportToDefaults.service.Contains(visibility.Private) {
				ps.ServiceIndex.privateByNamespace[ns] = append(ps.ServiceIndex.privateByNamespace[ns], s)
			} else if ps.exportToDefaults.service.Contains(visibility.Public) {
				ps.ServiceIndex.public = append(ps.ServiceIndex.public, s)
			}
		} else {
			// if service has exportTo *, make it public and ignore all other exportTos.
			// if service does not have exportTo *, but has exportTo ~ - i.e. not visible to anyone, ignore all exportTos.
			// if service has exportTo ., replace with current namespace.
			if s.Attributes.ExportTo.Contains(visibility.Public) {
				ps.ServiceIndex.public = append(ps.ServiceIndex.public, s)
				continue
			} else if s.Attributes.ExportTo.Contains(visibility.None) {
				continue
			}
			// . or other namespaces
			for exportTo := range s.Attributes.ExportTo {
				if exportTo == visibility.Private || string(exportTo) == ns {
					// exportTo with same namespace is effectively private
					ps.ServiceIndex.privateByNamespace[ns] = append(ps.ServiceIndex.privateByNamespace[ns], s)
				} else {
					// exportTo is a specific target namespace
					ps.ServiceIndex.exportedToNamespace[string(exportTo)] = append(ps.ServiceIndex.exportedToNamespace[string(exportTo)], s)
				}
			}
		}
	}

	ps.initServiceAccounts(env, allServices)
}

// resolveServiceAliases sets the Aliases attributes on all services. The incoming Service's will just have AliasFor set,
// but in our usage we often need the opposite: for a given service, what are all the aliases?
// resolveServiceAliases walks this 'graph' of services and updates the Alias field in-place.
func resolveServiceAliases(allServices []*Service, configsUpdated sets.Set[ConfigKey]) {
	// rawAlias builds a map of Service -> AliasFor. So this will be ExternalName -> Service.
	// In an edge case, we can have ExternalName -> ExternalName; we resolve that below.
	rawAlias := map[NamespacedHostname]host.Name{}
	for _, s := range allServices {
		if s.Resolution != Alias {
			continue
		}
		nh := NamespacedHostname{
			Hostname:  s.Hostname,
			Namespace: s.Attributes.Namespace,
		}
		rawAlias[nh] = host.Name(s.Attributes.K8sAttributes.ExternalName)
	}

	// unnamespacedRawAlias is like rawAlias but without namespaces.
	// This is because an `ExternalName` isn't namespaced. If there is a conflict, the behavior is undefined.
	// This is split from above as a minor optimization to right-size the map
	unnamespacedRawAlias := make(map[host.Name]host.Name, len(rawAlias))
	for k, v := range rawAlias {
		unnamespacedRawAlias[k.Hostname] = v
	}

	// resolvedAliases builds a map of Alias -> Concrete, fully resolving through multiple hops.
	// Ex: Alias1 -> Alias2 -> Concrete will flatten to Alias1 -> Concrete.
	resolvedAliases := make(map[NamespacedHostname]host.Name, len(rawAlias))
	for alias, referencedService := range rawAlias {
		// referencedService may be another alias or a concrete service.
		if _, f := unnamespacedRawAlias[referencedService]; !f {
			// Common case: alias pointing to a concrete service
			resolvedAliases[alias] = referencedService
			continue
		}
		// Otherwise, we need to traverse the alias "graph".
		// In an obscure edge case, a user could make a loop, so we will need to handle that.
		seen := sets.New(alias.Hostname, referencedService)
		for {
			n, f := unnamespacedRawAlias[referencedService]
			if !f {
				// The destination we are pointing to is not an alias, so this is the terminal step
				resolvedAliases[alias] = referencedService
				break
			}
			if seen.InsertContains(n) {
				// We did a loop!
				// Kubernetes will make these NXDomain, so we can just treat it like it doesn't exist at all
				break
			}
			referencedService = n
		}
	}

	// aliasesForService builds a map of Concrete -> []Aliases
	// This basically reverses our resolvedAliased map, which is Alias -> Concrete,
	aliasesForService := map[host.Name][]NamespacedHostname{}
	for alias, concrete := range resolvedAliases {
		aliasesForService[concrete] = append(aliasesForService[concrete], alias)

		// We also need to update configsUpdated, such that any "alias" updated also marks the concrete service as updated.
		aliasKey := ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      alias.Hostname.String(),
			Namespace: alias.Namespace,
		}
		// Alias. We should mark all the concrete services as updated as well.
		if configsUpdated.Contains(aliasKey) {
			// We only have the hostname, but we need the namespace...
			for _, svc := range allServices {
				if svc.Hostname == concrete {
					configsUpdated.Insert(ConfigKey{
						Kind:      kind.ServiceEntry,
						Name:      concrete.String(),
						Namespace: svc.Attributes.Namespace,
					})
				}
			}
		}
	}
	// Sort aliases so order is deterministic.
	for _, v := range aliasesForService {
		slices.SortFunc(v, func(a, b NamespacedHostname) int {
			if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
				return r
			}
			return cmp.Compare(a.Hostname, b.Hostname)
		})
	}

	// Finally, we can traverse all services and update the ones that have aliases
	for i, s := range allServices {
		if aliases, f := aliasesForService[s.Hostname]; f {
			// This service has an alias; set it. We need to make a copy since the underlying Service is shared
			s = s.DeepCopy()
			s.Attributes.Aliases = aliases
			allServices[i] = s
		}
	}
}

// SortServicesByCreationTime sorts the list of services in ascending order by their creation time (if available).
func SortServicesByCreationTime(services []*Service) []*Service {
	slices.SortStableFunc(services, func(i, j *Service) int {
		if r := i.CreationTime.Compare(j.CreationTime); r != 0 {
			return r
		}
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(i.Attributes.Name, j.Attributes.Name); r != 0 {
			return r
		}
		return cmp.Compare(i.Attributes.Namespace, j.Attributes.Namespace)
	})
	return services
}

// Caches list of service accounts in the registry
func (ps *PushContext) initServiceAccounts(env *Environment, services []*Service) {
	for _, svc := range services {
		var accounts sets.String
		// First get endpoint level service accounts
		shard, f := env.EndpointIndex.ShardsForService(string(svc.Hostname), svc.Attributes.Namespace)
		if f {
			shard.RLock()
			// copy here to reduce the lock time
			// endpoints could update frequently, so the longer it locks, the more likely it will block other threads.
			accounts = shard.ServiceAccounts.Copy()
			shard.RUnlock()
		}
		if len(svc.ServiceAccounts) > 0 {
			if accounts == nil {
				accounts = sets.New(svc.ServiceAccounts...)
			} else {
				accounts = accounts.InsertAll(svc.ServiceAccounts...)
			}
		}
		sa := sets.SortedList(spiffe.ExpandWithTrustDomains(accounts, ps.Mesh.TrustDomainAliases))
		key := serviceAccountKey{
			hostname:  svc.Hostname,
			namespace: svc.Attributes.Namespace,
		}
		ps.serviceAccounts[key] = sa
	}
}

// Caches list of authentication policies
func (ps *PushContext) initAuthnPolicies(env *Environment) {
	ps.AuthnPolicies = initAuthenticationPolicies(env)
}

// Caches list of virtual services
func (ps *PushContext) initVirtualServices(env *Environment) {
	ps.virtualServiceIndex.exportedToNamespaceByGateway = map[types.NamespacedName][]config.Config{}
	ps.virtualServiceIndex.privateByNamespaceAndGateway = map[types.NamespacedName][]config.Config{}
	ps.virtualServiceIndex.publicByGateway = map[string][]config.Config{}
	ps.virtualServiceIndex.referencedDestinations = map[string]sets.String{}

	if features.FilterGatewayClusterConfig {
		ps.virtualServiceIndex.destinationsByGateway = make(map[string]sets.String)
	}

	virtualServices := env.List(gvk.VirtualService, NamespaceAll)

	vservices := make([]config.Config, len(virtualServices))

	totalVirtualServices.Record(float64(len(virtualServices)))

	// convert all shortnames in virtual services into FQDNs
	for i, r := range virtualServices {
		vservices[i] = resolveVirtualServiceShortnames(r)
	}

	vservices, ps.virtualServiceIndex.delegates = mergeVirtualServicesIfNeeded(vservices, ps.exportToDefaults.virtualService)

	for _, virtualService := range vservices {
		ns := virtualService.Namespace
		rule := virtualService.Spec.(*networking.VirtualService)
		gwNames := getGatewayNames(rule)
		if len(rule.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			// We only honor ., *
			if ps.exportToDefaults.virtualService.Contains(visibility.Private) {
				// add to local namespace only
				private := ps.virtualServiceIndex.privateByNamespaceAndGateway
				for _, gw := range gwNames {
					n := types.NamespacedName{Namespace: ns, Name: gw}
					private[n] = append(private[n], virtualService)
				}
			} else if ps.exportToDefaults.virtualService.Contains(visibility.Public) {
				for _, gw := range gwNames {
					ps.virtualServiceIndex.publicByGateway[gw] = append(ps.virtualServiceIndex.publicByGateway[gw], virtualService)
				}
			}
		} else {
			exportToSet := sets.NewWithLength[visibility.Instance](len(rule.ExportTo))
			for _, e := range rule.ExportTo {
				exportToSet.Insert(visibility.Instance(e))
			}
			// if vs has exportTo ~ - i.e. not visible to anyone, ignore all exportTos
			// if vs has exportTo *, make public and ignore all other exportTos
			// if vs has exportTo ., replace with current namespace
			if exportToSet.Contains(visibility.Public) {
				for _, gw := range gwNames {
					ps.virtualServiceIndex.publicByGateway[gw] = append(ps.virtualServiceIndex.publicByGateway[gw], virtualService)
				}
			} else if !exportToSet.Contains(visibility.None) {
				// . or other namespaces
				for exportTo := range exportToSet {
					if exportTo == visibility.Private || string(exportTo) == ns {
						// add to local namespace only
						for _, gw := range gwNames {
							n := types.NamespacedName{Namespace: ns, Name: gw}
							ps.virtualServiceIndex.privateByNamespaceAndGateway[n] = append(ps.virtualServiceIndex.privateByNamespaceAndGateway[n], virtualService)
						}
					} else {
						exported := ps.virtualServiceIndex.exportedToNamespaceByGateway
						// add to local namespace only
						for _, gw := range gwNames {
							n := types.NamespacedName{Namespace: string(exportTo), Name: gw}
							exported[n] = append(exported[n], virtualService)
						}
					}
				}
			}
		}

		if features.FilterGatewayClusterConfig {
			for _, gw := range gwNames {
				if gw == constants.IstioMeshGateway {
					continue
				}
				if features.ScopeGatewayToNamespace {
					gatewayNamespace := ns
					if gwNs, _, found := strings.Cut(gw, "/"); found {
						gatewayNamespace = gwNs
					}
					for host := range virtualServiceDestinationsFilteredBySourceNamespace(rule, gatewayNamespace) {
						sets.InsertOrNew(ps.virtualServiceIndex.destinationsByGateway, gw, host)
					}
				} else {
					for host := range virtualServiceDestinations(rule) {
						sets.InsertOrNew(ps.virtualServiceIndex.destinationsByGateway, gw, host)
					}
				}

			}
		}

		// For mesh virtual services, build a map of host -> referenced destinations
		if features.EnableAmbientWaypoints && (len(rule.Gateways) == 0 || slices.Contains(rule.Gateways, constants.IstioMeshGateway)) {
			for host := range virtualServiceDestinations(rule) {
				for _, rhost := range rule.Hosts {
					if _, f := ps.virtualServiceIndex.referencedDestinations[rhost]; !f {
						ps.virtualServiceIndex.referencedDestinations[rhost] = sets.New[string]()
					}
					ps.virtualServiceIndex.referencedDestinations[rhost].Insert(host)
				}
			}
		}
	}
}

var meshGateways = []string{constants.IstioMeshGateway}

func getGatewayNames(vs *networking.VirtualService) []string {
	if len(vs.Gateways) == 0 {
		return meshGateways
	}
	res := make([]string, 0, len(vs.Gateways))
	res = append(res, vs.Gateways...)
	return res
}

func (ps *PushContext) initDefaultExportMaps() {
	ps.exportToDefaults.destinationRule = sets.New[visibility.Instance]()
	if ps.Mesh.DefaultDestinationRuleExportTo != nil {
		for _, e := range ps.Mesh.DefaultDestinationRuleExportTo {
			ps.exportToDefaults.destinationRule.Insert(visibility.Instance(e))
		}
	} else {
		// default to *
		ps.exportToDefaults.destinationRule.Insert(visibility.Public)
	}

	ps.exportToDefaults.service = sets.New[visibility.Instance]()
	if ps.Mesh.DefaultServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultServiceExportTo {
			ps.exportToDefaults.service.Insert(visibility.Instance(e))
		}
	} else {
		ps.exportToDefaults.service.Insert(visibility.Public)
	}

	ps.exportToDefaults.virtualService = sets.New[visibility.Instance]()
	if ps.Mesh.DefaultVirtualServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultVirtualServiceExportTo {
			ps.exportToDefaults.virtualService.Insert(visibility.Instance(e))
		}
	} else {
		ps.exportToDefaults.virtualService.Insert(visibility.Public)
	}
}

// initSidecarScopes synthesizes Sidecar CRDs into objects called
// SidecarScope.  The SidecarScope object is a semi-processed view of the
// service registry, and config state associated with the sidecar CRD. The
// scope contains a set of inbound and outbound listeners, services/configs
// per listener, etc. The sidecar scopes are precomputed based on the
// Sidecar API objects in each namespace. If there is no sidecar api object
// for a namespace, a default sidecarscope is assigned to the namespace
// which enables connectivity to all services in the mesh.
//
// When proxies connect to Pilot, we identify the sidecar scope associated
// with the proxy and derive listeners/routes/clusters based on the sidecar
// scope.
func (ps *PushContext) initSidecarScopes(env *Environment) {
	rawSidecarConfigs := env.List(gvk.Sidecar, NamespaceAll)

	sortConfigByCreationTime(rawSidecarConfigs)

	sidecarConfigs := make([]config.Config, 0, len(rawSidecarConfigs))
	for _, sidecarConfig := range rawSidecarConfigs {
		sidecar := sidecarConfig.Spec.(*networking.Sidecar)
		// sidecars with selector take preference
		if sidecar.WorkloadSelector != nil {
			sidecarConfigs = append(sidecarConfigs, sidecarConfig)
		}
	}
	for _, sidecarConfig := range rawSidecarConfigs {
		sidecar := sidecarConfig.Spec.(*networking.Sidecar)
		// sidecars without selector placed behind
		if sidecar.WorkloadSelector == nil {
			sidecarConfigs = append(sidecarConfigs, sidecarConfig)
		}
	}

	// Hold reference root namespace's sidecar config
	// Root namespace can have only one sidecar config object
	// Currently we expect that it has no workloadSelectors
	var rootNSConfig *config.Config
	for i, sidecarConfig := range sidecarConfigs {
		if sidecarConfig.Namespace == ps.Mesh.RootNamespace &&
			sidecarConfig.Spec.(*networking.Sidecar).WorkloadSelector == nil {
			rootNSConfig = &sidecarConfigs[i]
			break
		}
	}
	ps.sidecarIndex.meshRootSidecarConfig = rootNSConfig

	ps.sidecarIndex.sidecarsByNamespace = make(map[string][]*SidecarScope)
	ps.convertSidecarScopes(sidecarConfigs)
}

func (ps *PushContext) convertSidecarScopes(sidecarConfigs []config.Config) {
	if len(sidecarConfigs) == 0 {
		return
	}
	if features.ConvertSidecarScopeConcurrency > 1 {
		ps.concurrentConvertToSidecarScope(sidecarConfigs)
	} else {
		for _, sidecarConfig := range sidecarConfigs {
			ps.sidecarIndex.sidecarsByNamespace[sidecarConfig.Namespace] = append(ps.sidecarIndex.sidecarsByNamespace[sidecarConfig.Namespace],
				convertToSidecarScope(ps, &sidecarConfig, sidecarConfig.Namespace))
		}
	}
}

func (ps *PushContext) concurrentConvertToSidecarScope(sidecarConfigs []config.Config) {
	type taskItem struct {
		idx int
		cfg config.Config
	}

	var wg sync.WaitGroup
	taskItems := make(chan taskItem)
	sidecarScopes := make([]*SidecarScope, len(sidecarConfigs))
	for i := 0; i < features.ConvertSidecarScopeConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				item, ok := <-taskItems
				if !ok {
					break
				}
				sc := convertToSidecarScope(ps, &item.cfg, item.cfg.Namespace)
				sidecarScopes[item.idx] = sc
			}
		}()
	}

	// note: sidecarScopes order matters and needs to be kept in the same order as sidecarConfigs.
	// The order indicates priority, see getSidecarScope.
	for idx, cfg := range sidecarConfigs {
		taskItems <- taskItem{idx: idx, cfg: cfg}
	}

	close(taskItems)
	wg.Wait()

	for _, sc := range sidecarScopes {
		ps.sidecarIndex.sidecarsByNamespace[sc.Namespace] = append(ps.sidecarIndex.sidecarsByNamespace[sc.Namespace], sc)
	}
}

// Split out of DestinationRule expensive conversions - once per push.
func (ps *PushContext) initDestinationRules(env *Environment) {
	configs := env.List(gvk.DestinationRule, NamespaceAll)

	// values returned from ConfigStore.List are immutable.
	// Therefore, we make a copy
	destRules := make([]config.Config, len(configs))
	for i := range destRules {
		destRules[i] = configs[i]
	}

	ps.setDestinationRules(destRules)
}

func newConsolidatedDestRules() *consolidatedDestRules {
	return &consolidatedDestRules{
		specificDestRules: map[host.Name][]*ConsolidatedDestRule{},
		wildcardDestRules: map[host.Name][]*ConsolidatedDestRule{},
	}
}

// Testing Only. This allows tests to inject a config without having the mock.
func (ps *PushContext) SetDestinationRulesForTesting(configs []config.Config) {
	ps.setDestinationRules(configs)
}

// sortConfigBySelectorAndCreationTime sorts the list of config objects based on priority and creation time.
func sortConfigBySelectorAndCreationTime(configs []config.Config) []config.Config {
	sort.Slice(configs, func(i, j int) bool {
		// check if one of the configs has priority
		idr := configs[i].Spec.(*networking.DestinationRule)
		jdr := configs[j].Spec.(*networking.DestinationRule)
		if idr.GetWorkloadSelector() != nil && jdr.GetWorkloadSelector() == nil {
			return true
		}
		if idr.GetWorkloadSelector() == nil && jdr.GetWorkloadSelector() != nil {
			return false
		}

		// If priority is the same or neither has priority, fallback to creation time ordering
		if r := configs[i].CreationTimestamp.Compare(configs[j].CreationTimestamp); r != 0 {
			return r == -1 // -1 means i is less than j, so return true.
		}
		if r := cmp.Compare(configs[i].Name, configs[j].Name); r != 0 {
			return r == -1
		}
		return cmp.Compare(configs[i].Namespace, configs[j].Namespace) == -1
	})
	return configs
}

// setDestinationRules updates internal structures using a set of configs.
// Split out of DestinationRule expensive conversions, computed once per push.
// This will not work properly for Sidecars, which will precompute their
// destination rules on init.
func (ps *PushContext) setDestinationRules(configs []config.Config) {
	// Sort by time first. So if two destination rule have top level traffic policies
	// we take the first one.
	sortConfigBySelectorAndCreationTime(configs)
	namespaceLocalDestRules := make(map[string]*consolidatedDestRules)
	exportedDestRulesByNamespace := make(map[string]*consolidatedDestRules)
	rootNamespaceLocalDestRules := newConsolidatedDestRules()

	for i := range configs {
		rule := configs[i].Spec.(*networking.DestinationRule)

		rule.Host = string(ResolveShortnameToFQDN(rule.Host, configs[i].Meta))
		var exportToSet sets.Set[visibility.Instance]

		// destination rules with workloadSelector should not be exported to other namespaces
		if rule.GetWorkloadSelector() == nil {
			exportToSet = sets.NewWithLength[visibility.Instance](len(rule.ExportTo))
			for _, e := range rule.ExportTo {
				exportToSet.Insert(visibility.Instance(e))
			}
		} else {
			exportToSet = sets.New[visibility.Instance](visibility.Private)
		}

		// add only if the dest rule is exported with . or * or explicit exportTo containing this namespace
		// The global exportTo doesn't matter here (its either . or * - both of which are applicable here)
		if exportToSet.IsEmpty() || exportToSet.Contains(visibility.Public) || exportToSet.Contains(visibility.Private) ||
			exportToSet.Contains(visibility.Instance(configs[i].Namespace)) {
			// Store in an index for the config's namespace
			// a proxy from this namespace will first look here for the destination rule for a given service
			// This pool consists of both public/private destination rules.
			if _, exist := namespaceLocalDestRules[configs[i].Namespace]; !exist {
				namespaceLocalDestRules[configs[i].Namespace] = newConsolidatedDestRules()
			}
			// Merge this destination rule with any public/private dest rules for same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			ps.mergeDestinationRule(namespaceLocalDestRules[configs[i].Namespace], configs[i], exportToSet)
		}

		isPrivateOnly := false
		// No exportTo in destinationRule. Use the global default
		// We only honor . and *
		if exportToSet.IsEmpty() && ps.exportToDefaults.destinationRule.Contains(visibility.Private) {
			isPrivateOnly = true
		} else if exportToSet.Len() == 1 && (exportToSet.Contains(visibility.Private) || exportToSet.Contains(visibility.Instance(configs[i].Namespace))) {
			isPrivateOnly = true
		}

		if !isPrivateOnly {
			if _, exist := exportedDestRulesByNamespace[configs[i].Namespace]; !exist {
				exportedDestRulesByNamespace[configs[i].Namespace] = newConsolidatedDestRules()
			}
			// Merge this destination rule with any other exported dest rule for the same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			ps.mergeDestinationRule(exportedDestRulesByNamespace[configs[i].Namespace], configs[i], exportToSet)
		} else if configs[i].Namespace == ps.Mesh.RootNamespace {
			// Keep track of private root namespace destination rules
			ps.mergeDestinationRule(rootNamespaceLocalDestRules, configs[i], exportToSet)
		}
	}

	ps.destinationRuleIndex.namespaceLocal = namespaceLocalDestRules
	ps.destinationRuleIndex.exportedByNamespace = exportedDestRulesByNamespace
	ps.destinationRuleIndex.rootNamespaceLocal = rootNamespaceLocalDestRules
}

// pre computes all AuthorizationPolicies per namespace
func (ps *PushContext) initAuthorizationPolicies(env *Environment) {
	ps.AuthzPolicies = GetAuthorizationPolicies(env)
}

func (ps *PushContext) initTelemetry(env *Environment) {
	ps.Telemetry = getTelemetries(env)
}

func (ps *PushContext) initProxyConfigs(env *Environment) {
	ps.ProxyConfigs = GetProxyConfigs(env.ConfigStore, env.Mesh())
}

// pre computes WasmPlugins per namespace
func (ps *PushContext) initWasmPlugins(env *Environment) {
	wasmplugins := env.List(gvk.WasmPlugin, NamespaceAll)

	sortConfigByCreationTime(wasmplugins)
	ps.wasmPluginsByNamespace = map[string][]*WasmPluginWrapper{}
	for _, plugin := range wasmplugins {
		if pluginWrapper := convertToWasmPluginWrapper(plugin); pluginWrapper != nil {
			ps.wasmPluginsByNamespace[plugin.Namespace] = append(ps.wasmPluginsByNamespace[plugin.Namespace], pluginWrapper)
		}
	}
}

// WasmPlugins return the WasmPluginWrappers of a proxy.
// For most proxy types, we include only the root namespace and same-namespace objects.
// However, waypoints allow cross-namespace access based on attached Service objects.
// In this case, include all referenced services in the selection criteria
func (ps *PushContext) WasmPlugins(proxy *Proxy) map[extensions.PluginPhase][]*WasmPluginWrapper {
	listenerInfo := WasmPluginListenerInfo{}
	if proxy.IsWaypointProxy() {
		servicesInfo := ps.ServicesForWaypoint(WaypointKeyForProxy(proxy))
		for _, si := range servicesInfo {
			s := si.Service
			svc, exist := ps.ServiceIndex.HostnameAndNamespace[host.Name(s.Hostname)][s.Namespace]
			if !exist {
				log.Warnf("cannot find waypoint service in serviceindex, namespace/hostname: %s/%s", s.Namespace, s.Hostname)
				continue
			}
			listenerInfo = listenerInfo.WithService(svc)
		}
	}
	return ps.WasmPluginsByListenerInfo(proxy, listenerInfo, WasmPluginTypeAny)
}

func (ps *PushContext) WasmPluginsByName(proxy *Proxy, names []types.NamespacedName) []*WasmPluginWrapper {
	res := make([]*WasmPluginWrapper, 0, len(names))
	for _, n := range names {
		if n.Namespace != proxy.ConfigNamespace && n.Namespace != ps.Mesh.RootNamespace {
			log.Warnf("proxy requested invalid WASM configuration: %v", n)
			continue
		}
		for _, wsm := range ps.wasmPluginsByNamespace[n.Namespace] {
			if wsm.Name == n.Name {
				res = append(res, wsm)
				break
			}
		}
	}
	return res
}

// WasmPluginsByListenerInfo return the WasmPluginWrappers which are matched with TrafficSelector in the given proxy.
func (ps *PushContext) WasmPluginsByListenerInfo(proxy *Proxy, info WasmPluginListenerInfo,
	pluginType WasmPluginType,
) map[extensions.PluginPhase][]*WasmPluginWrapper {
	if proxy == nil {
		return nil
	}

	matchedPlugins := make(map[extensions.PluginPhase][]*WasmPluginWrapper)
	lookupInNamespaces := []string{proxy.ConfigNamespace, ps.Mesh.RootNamespace}
	for i := range info.Services {
		lookupInNamespaces = append(lookupInNamespaces, info.Services[i].NamespacedName().Namespace)
	}
	selectionOpts := PolicyMatcherForProxy(proxy).WithServices(info.Services).WithRootNamespace(ps.Mesh.GetRootNamespace())
	for _, ns := range slices.FilterDuplicates(lookupInNamespaces) {
		if wasmPlugins, ok := ps.wasmPluginsByNamespace[ns]; ok {
			for _, plugin := range wasmPlugins {
				if plugin.MatchListener(selectionOpts, info) && plugin.MatchType(pluginType) {
					matchedPlugins[plugin.Phase] = append(matchedPlugins[plugin.Phase], plugin)
				}
			}
		}
	}

	// sort slices by priority
	for i, slice := range matchedPlugins {
		sort.SliceStable(slice, func(i, j int) bool {
			iPriority := int32(math.MinInt32)
			if prio := slice[i].Priority; prio != nil {
				iPriority = prio.Value
			}
			jPriority := int32(math.MinInt32)
			if prio := slice[j].Priority; prio != nil {
				jPriority = prio.Value
			}
			return iPriority > jPriority
		})
		matchedPlugins[i] = slice
	}

	return matchedPlugins
}

// pre computes envoy filters per namespace
func (ps *PushContext) initEnvoyFilters(env *Environment, changed sets.Set[ConfigKey], previousIndex map[string][]*EnvoyFilterWrapper) {
	envoyFilterConfigs := env.List(gvk.EnvoyFilter, NamespaceAll)
	previous := make(map[ConfigKey]*EnvoyFilterWrapper)
	for namespace, nsEnvoyFilters := range previousIndex {
		for _, envoyFilter := range nsEnvoyFilters {
			previous[ConfigKey{Kind: kind.EnvoyFilter, Namespace: namespace, Name: envoyFilter.Name}] = envoyFilter
		}
	}

	sort.Slice(envoyFilterConfigs, func(i, j int) bool {
		ifilter := envoyFilterConfigs[i].Spec.(*networking.EnvoyFilter)
		jfilter := envoyFilterConfigs[j].Spec.(*networking.EnvoyFilter)
		if ifilter.Priority != jfilter.Priority {
			return ifilter.Priority < jfilter.Priority
		}
		// If priority is same fallback to name and creation timestamp, else use priority.
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name and namespace, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if envoyFilterConfigs[i].CreationTimestamp != envoyFilterConfigs[j].CreationTimestamp {
			return envoyFilterConfigs[i].CreationTimestamp.Before(envoyFilterConfigs[j].CreationTimestamp)
		}
		in := envoyFilterConfigs[i].Name + "." + envoyFilterConfigs[i].Namespace
		jn := envoyFilterConfigs[j].Name + "." + envoyFilterConfigs[j].Namespace
		return in < jn
	})

	for _, envoyFilterConfig := range envoyFilterConfigs {
		var efw *EnvoyFilterWrapper
		key := ConfigKey{Kind: kind.EnvoyFilter, Namespace: envoyFilterConfig.Namespace, Name: envoyFilterConfig.Name}
		if prev, ok := previous[key]; ok && !changed.Contains(key) {
			// Reuse the previous EnvoyFilterWrapper if it exists and hasn't changed when optimized config rebuild is enabled
			efw = prev
		}
		// Rebuild the envoy filter in all other cases.
		if efw == nil {
			efw = convertToEnvoyFilterWrapper(&envoyFilterConfig)
		}
		ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace] = append(ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace], efw)
	}
}

type MergedEnvoyFilterWrapper struct {
	Patches map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper

	ReferencedNamespacedServices sets.Set[NamespacedHostname]
	ReferencedServices           sets.String
}

// EnvoyFilters return the merged EnvoyFilterWrapper of a proxy
func (ps *PushContext) EnvoyFilters(proxy *Proxy) *MergedEnvoyFilterWrapper {
	// this should never happen
	if proxy == nil {
		return nil
	}
	var matchedEnvoyFilters []*EnvoyFilterWrapper
	// EnvoyFilters supports inheritance (global ones plus namespace local ones).
	// First get all the filter configs from the config root namespace
	// and then add the ones from proxy's own namespace
	if ps.Mesh.RootNamespace != "" {
		matchedEnvoyFilters = ps.getMatchedEnvoyFilters(proxy, ps.Mesh.RootNamespace)
	}

	// To prevent duplicate envoyfilters in case root namespace equals proxy's namespace
	if proxy.ConfigNamespace != ps.Mesh.RootNamespace {
		matched := ps.getMatchedEnvoyFilters(proxy, proxy.ConfigNamespace)
		matchedEnvoyFilters = append(matchedEnvoyFilters, matched...)
	}

	sort.Slice(matchedEnvoyFilters, func(i, j int) bool {
		ifilter := matchedEnvoyFilters[i]
		jfilter := matchedEnvoyFilters[j]
		if ifilter.Priority != jfilter.Priority {
			return ifilter.Priority < jfilter.Priority
		}
		// Prefer root namespace filters over non-root namespace filters.
		if ifilter.Namespace != jfilter.Namespace &&
			(ifilter.Namespace == ps.Mesh.RootNamespace || jfilter.Namespace == ps.Mesh.RootNamespace) {
			return ifilter.Namespace == ps.Mesh.RootNamespace
		}
		if ifilter.creationTime != jfilter.creationTime {
			return ifilter.creationTime.Before(jfilter.creationTime)
		}
		in := ifilter.Name + "." + ifilter.Namespace
		jn := jfilter.Name + "." + jfilter.Namespace
		return in < jn
	})
	var out *MergedEnvoyFilterWrapper
	if len(matchedEnvoyFilters) > 0 {
		out = &MergedEnvoyFilterWrapper{
			ReferencedNamespacedServices: sets.New[NamespacedHostname](),
			ReferencedServices:           sets.New[string](),
			// no need populate workloadSelector, as it is not used later.
			Patches: make(map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper),
		}
		// merge EnvoyFilterWrapper
		for _, efw := range matchedEnvoyFilters {
			out.ReferencedNamespacedServices.Merge(efw.ReferencedNamespacedServices)
			out.ReferencedServices.Merge(efw.ReferencedServices)
			for applyTo, cps := range efw.Patches {
				for _, cp := range cps {
					if proxyMatch(proxy, cp) {
						out.Patches[applyTo] = append(out.Patches[applyTo], cp)
					}
				}
			}
		}
	}

	return out
}

// if there is no workload selector, the config applies to all workloads
// if there is a workload selector, check for matching workload labels
func (ps *PushContext) getMatchedEnvoyFilters(proxy *Proxy, namespaces string) []*EnvoyFilterWrapper {
	matchedEnvoyFilters := make([]*EnvoyFilterWrapper, 0)
	for _, efw := range ps.envoyFiltersByNamespace[namespaces] {
		if efw.GetTargetRefs() != nil {
			// These are meant for a specific target, so we shouldn't treat these as "always match"
			// In the future, targetRef for EnvoyFilter will likely be implemented -- but currently these would never match
			continue
		}
		if efw.workloadSelector == nil || efw.workloadSelector.SubsetOf(proxy.Labels) {
			matchedEnvoyFilters = append(matchedEnvoyFilters, efw)
		}
	}
	return matchedEnvoyFilters
}

// HasEnvoyFilters checks if an EnvoyFilter exists with the given name at the given namespace.
func (ps *PushContext) HasEnvoyFilters(name, namespace string) bool {
	for _, efw := range ps.envoyFiltersByNamespace[namespace] {
		if efw.Name == name {
			return true
		}
	}
	return false
}

// pre computes gateways per namespace
func (ps *PushContext) initGateways(env *Environment) {
	gatewayConfigs := env.List(gvk.Gateway, NamespaceAll)

	sortConfigByCreationTime(gatewayConfigs)

	if features.ScopeGatewayToNamespace {
		ps.gatewayIndex.namespace = make(map[string][]config.Config)
		for _, gatewayConfig := range gatewayConfigs {
			if _, exists := ps.gatewayIndex.namespace[gatewayConfig.Namespace]; !exists {
				ps.gatewayIndex.namespace[gatewayConfig.Namespace] = make([]config.Config, 0)
			}
			ps.gatewayIndex.namespace[gatewayConfig.Namespace] = append(ps.gatewayIndex.namespace[gatewayConfig.Namespace], gatewayConfig)
		}
	} else {
		ps.gatewayIndex.all = gatewayConfigs
	}
}

func (ps *PushContext) initAmbient(env *Environment) {
	ps.ambientIndex = env
}

// InternalGatewayServiceAnnotation represents the hostname of the service a gateway will use. This is
// only used internally to transfer information from the Kubernetes Gateway API to the Istio Gateway API
// which does not have a field to represent this.
// The format is a comma separated list of hostnames. For example, "ingress.istio-system.svc.cluster.local,ingress.example.com"
// The Gateway will apply to all ServiceInstances of these services, *in the same namespace as the Gateway*.
const InternalGatewayServiceAnnotation = "internal.istio.io/gateway-service"

type gatewayWithInstances struct {
	gateway config.Config
	// If true, ports that are not present in any instance will be used directly (without targetPort translation)
	// This supports the legacy behavior of selecting gateways by pod label selector
	legacyGatewaySelector bool
	instances             []ServiceTarget
}

func (ps *PushContext) mergeGateways(proxy *Proxy) *MergedGateway {
	// this should never happen
	if proxy == nil {
		return nil
	}
	gatewayInstances := make([]gatewayWithInstances, 0)

	var configs []config.Config
	if features.ScopeGatewayToNamespace {
		configs = ps.gatewayIndex.namespace[proxy.ConfigNamespace]
	} else {
		configs = ps.gatewayIndex.all
	}

	for _, cfg := range configs {
		gw := cfg.Spec.(*networking.Gateway)
		if gwsvcstr, f := cfg.Annotations[InternalGatewayServiceAnnotation]; f {
			gwsvcs := strings.Split(gwsvcstr, ",")
			known := sets.New[string](gwsvcs...)
			matchingInstances := make([]ServiceTarget, 0, len(proxy.ServiceTargets))
			for _, si := range proxy.ServiceTargets {
				if _, f := known[string(si.Service.Hostname)]; f && si.Service.Attributes.Namespace == cfg.Namespace {
					matchingInstances = append(matchingInstances, si)
				}
			}
			// Only if we have a matching instance should we apply the configuration
			if len(matchingInstances) > 0 {
				gatewayInstances = append(gatewayInstances, gatewayWithInstances{cfg, false, matchingInstances})
			}
		} else if gw.GetSelector() == nil {
			// no selector. Applies to all workloads asking for the gateway
			gatewayInstances = append(gatewayInstances, gatewayWithInstances{cfg, true, proxy.ServiceTargets})
		} else {
			gatewaySelector := labels.Instance(gw.GetSelector())
			if gatewaySelector.SubsetOf(proxy.Labels) {
				gatewayInstances = append(gatewayInstances, gatewayWithInstances{cfg, true, proxy.ServiceTargets})
			}
		}
	}

	if len(gatewayInstances) == 0 {
		return nil
	}

	return mergeGateways(gatewayInstances, proxy, ps)
}

func (ps *PushContext) NetworkManager() *NetworkManager {
	return ps.networkMgr
}

// AllInstancesSupportHBONE checks whether all instances of a service support HBONE. This is used in cases where we need
// to decide if we are always going to send HBONE, so we can set service-level properties.
// Mostly this works around limitations in the dataplane that don't support per-endpoint properties we would want to be
// per-endpoint.
func (ps *PushContext) AllInstancesSupportHBONE(service *Service, port *Port) bool {
	instances := ps.ServiceEndpointsByPort(service, port.Port, nil)
	if len(instances) == 0 {
		return false
	}
	for _, e := range instances {
		addressSupportsTunnel := false
		if SupportsTunnel(e.Labels, TunnelHTTP) {
			addressSupportsTunnel = true
		} else {
			for _, addr := range e.Addresses {
				if ps.SupportsTunnel(e.Network, addr) {
					addressSupportsTunnel = true
					break
				}
			}
		}
		if !addressSupportsTunnel {
			return false
		}
	}
	return true
}

// BestEffortInferServiceMTLSMode infers the mTLS mode for the service + port from all authentication
// policies (both alpha and beta) in the system. The function always returns MTLSUnknown for external service.
// The result is a best effort. It is because the PeerAuthentication is workload-based, this function is unable
// to compute the correct service mTLS mode without knowing service to workload binding. For now, this
// function uses only mesh and namespace level PeerAuthentication and ignore workload & port level policies.
// This function is used to give a hint for auto-mTLS configuration on client side.
func (ps *PushContext) BestEffortInferServiceMTLSMode(tp *networking.TrafficPolicy, service *Service, port *Port) MutualTLSMode {
	if service.MeshExternal {
		// Only need the authentication mTLS mode when service is not external.
		return MTLSUnknown
	}

	// For passthrough traffic (headless service or explicitly defined in DestinationRule), we look at the instances
	// If ALL instances have a sidecar, we enable TLS, otherwise we disable
	// TODO(https://github.com/istio/istio/issues/27376) enable mixed deployments
	// A service with passthrough resolution is always passthrough, regardless of the TrafficPolicy.
	if service.Resolution == Passthrough || tp.GetLoadBalancer().GetSimple() == networking.LoadBalancerSettings_PASSTHROUGH {
		instances := ps.ServiceEndpointsByPort(service, port.Port, nil)
		if len(instances) == 0 {
			return MTLSDisable
		}
		for _, i := range instances {
			// Infer mTls disabled if any of the endpoint is with tls disabled
			if i.TLSMode == DisabledTLSModeLabel {
				return MTLSDisable
			}
		}
	}

	// 2. check mTLS settings from beta policy (i.e PeerAuthentication) at namespace / mesh level.
	// If the mode is not unknown, use it.
	if serviceMTLSMode := ps.AuthnPolicies.GetNamespaceMutualTLSMode(service.Attributes.Namespace); serviceMTLSMode != MTLSUnknown {
		return serviceMTLSMode
	}

	// Fallback to permissive.
	return MTLSPermissive
}

// ServiceEndpointsByPort returns the cached instances by port if it exists.
func (ps *PushContext) ServiceEndpointsByPort(svc *Service, port int, labels labels.Instance) []*IstioEndpoint {
	var out []*IstioEndpoint
	if instances, exists := ps.ServiceIndex.instancesByPort[svc.Key()][port]; exists {
		// Use cached version of instances by port when labels are empty.
		if len(labels) == 0 {
			return instances
		}
		// If there are labels,	we will filter instances by pod labels.
		for _, instance := range instances {
			// check that one of the input labels is a subset of the labels
			if labels.SubsetOf(instance.Labels) {
				out = append(out, instance)
			}
		}
	}

	return out
}

// ServiceEndpoints returns the cached instances by svc if exists.
func (ps *PushContext) ServiceEndpoints(svcKey string) map[int][]*IstioEndpoint {
	if instances, exists := ps.ServiceIndex.instancesByPort[svcKey]; exists {
		return instances
	}

	return nil
}

// initKubernetesGateways initializes Kubernetes gateway-api objects
func (ps *PushContext) initKubernetesGateways(env *Environment) {
	if env.GatewayAPIController != nil {
		ps.GatewayAPIController = env.GatewayAPIController
		env.GatewayAPIController.Reconcile(ps)
	}
}

// ReferenceAllowed determines if a given resource (of type `kind` and name `resourceName`) can be
// accessed by `namespace`, based of specific reference policies.
// Note: this function only determines if a reference is *explicitly* allowed; the reference may not require
// explicit authorization to be made at all in most cases. Today, this only is for allowing cross-namespace
// secret access.
func (ps *PushContext) ReferenceAllowed(kind config.GroupVersionKind, resourceName string, namespace string) bool {
	// Currently, only Secret has reference policy, and only implemented by Gateway API controller.
	switch kind {
	case gvk.Secret:
		if ps.GatewayAPIController != nil {
			return ps.GatewayAPIController.SecretAllowed(resourceName, namespace)
		}
	default:
	}
	return false
}

func (ps *PushContext) ServiceAccounts(hostname host.Name, namespace string) []string {
	return ps.serviceAccounts[serviceAccountKey{
		hostname:  hostname,
		namespace: namespace,
	}]
}

// SupportsTunnel checks if a given IP address supports tunneling.
// This currently only accepts workload IPs as arguments; services will always return "false".
func (ps *PushContext) SupportsTunnel(n network.ID, ip string) bool {
	// There should be a 1:1 relationship between IP and Workload but the interface doesn't allow this lookup.
	// We should get 0 or 1 workloads, so just return the first.
	infos, _ := ps.ambientIndex.AddressInformation(sets.New(n.String() + "/" + ip))
	for _, wl := range ExtractWorkloadsFromAddresses(infos) {
		if wl.Workload.TunnelProtocol == workloadapi.TunnelProtocol_HBONE {
			return true
		}
	}
	return false
}

// WorkloadsForWaypoint returns all workloads associated with a given waypoint identified by it's WaypointKey
// Used when calculating the workloads which should be configured for a specific waypoint proxy
func (ps *PushContext) WorkloadsForWaypoint(key WaypointKey) []WorkloadInfo {
	return ps.ambientIndex.WorkloadsForWaypoint(key)
}

// ServicesForWaypoint returns all services associated with a given waypoint identified by it's WaypointKey
// Used when calculating the services which should be configured for a specific waypoint proxy
func (ps *PushContext) ServicesForWaypoint(key WaypointKey) []ServiceInfo {
	return ps.ambientIndex.ServicesForWaypoint(key)
}

// ServicesWithWaypoint returns all services associated with any waypoint.
// Key can optionally be provided in the form 'namespace/hostname'. If unset, all are returned
func (ps *PushContext) ServicesWithWaypoint(key string) []ServiceWaypointInfo {
	return ps.ambientIndex.ServicesWithWaypoint(key)
}
