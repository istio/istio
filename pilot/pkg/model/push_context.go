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
	"encoding/json"
	"net"
	"sort"
	"sync"
	"time"

	"go.uber.org/atomic"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/pkg/monitoring"
)

// Metrics is an interface for capturing metrics on a per-node basis.
type Metrics interface {
	// AddMetric will add an case to the metric for the given node.
	AddMetric(metric monitoring.Metric, key string, proxyID, msg string)
}

var _ Metrics = &PushContext{}

// serviceIndex is an index of all services by various fields for easy access during push.
type serviceIndex struct {
	// privateServices are reachable within the same namespace, with exportTo "."
	privateByNamespace map[string][]*Service
	// public are services reachable within the mesh with exportTo "*"
	public []*Service
	// exportedToNamespace are services that were made visible to this namespace
	// by an exportTo explicitly specifying this namespace.
	exportedToNamespace map[string][]*Service

	// HostnameAndNamespace has all services, indexed by hostname then namespace.
	HostnameAndNamespace map[host.Name]map[string]*Service `json:"-"`

	// instancesByPort contains a map of service and instances by port. It is stored here
	// to avoid recomputations during push. This caches instanceByPort calls with empty labels.
	// Call InstancesByPort directly when instances need to be filtered by actual labels.
	instancesByPort map[*Service]map[int][]*ServiceInstance
}

func newServiceIndex() serviceIndex {
	return serviceIndex{
		public:               []*Service{},
		privateByNamespace:   map[string][]*Service{},
		exportedToNamespace:  map[string][]*Service{},
		HostnameAndNamespace: map[host.Name]map[string]*Service{},
		instancesByPort:      map[*Service]map[int][]*ServiceInstance{},
	}
}

// exportToDefaults contains the default exportTo values.
type exportToDefaults struct {
	service         map[visibility.Instance]bool
	virtualService  map[visibility.Instance]bool
	destinationRule map[visibility.Instance]bool
}

// virtualServiceIndex is the index of virtual services by various fields.
type virtualServiceIndex struct {
	exportedToNamespaceByGateway map[string]map[string][]config.Config
	// this contains all the virtual services with exportTo "." and current namespace. The keys are namespace,gateway.
	privateByNamespaceAndGateway map[string]map[string][]config.Config
	// This contains all virtual services whose exportTo is "*", keyed by gateway
	publicByGateway map[string][]config.Config
	// root vs namespace/name ->delegate vs virtualservice gvk/namespace/name
	delegates map[ConfigKey][]ConfigKey
}

func newVirtualServiceIndex() virtualServiceIndex {
	return virtualServiceIndex{
		publicByGateway:              map[string][]config.Config{},
		privateByNamespaceAndGateway: map[string]map[string][]config.Config{},
		exportedToNamespaceByGateway: map[string]map[string][]config.Config{},
		delegates:                    map[ConfigKey][]ConfigKey{},
	}
}

// destinationRuleIndex is the index of destination rules by various fields.
type destinationRuleIndex struct {
	//  namespaceLocal contains all public/private dest rules pertaining to a service defined in a given namespace.
	namespaceLocal map[string]*processedDestRules
	//  exportedByNamespace contains all dest rules pertaining to a service exported by a namespace.
	exportedByNamespace map[string]*processedDestRules
	rootNamespaceLocal  *processedDestRules
	// mesh/namespace dest rules to be inherited
	inheritedByNamespace map[string]*config.Config
}

func newDestinationRuleIndex() destinationRuleIndex {
	return destinationRuleIndex{
		namespaceLocal:       map[string]*processedDestRules{},
		exportedByNamespace:  map[string]*processedDestRules{},
		inheritedByNamespace: map[string]*config.Config{},
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

	// ServiceAccounts contains a map of hostname and port to service accounts.
	ServiceAccounts map[host.Name]map[int][]string `json:"-"`

	// virtualServiceIndex is the index of virtual services by various fields.
	virtualServiceIndex virtualServiceIndex

	// destinationRuleIndex is the index of destination rules by various fields.
	destinationRuleIndex destinationRuleIndex

	// gatewayIndex is the index of gateways.
	gatewayIndex gatewayIndex

	// clusterLocalHosts extracted from the MeshConfig
	clusterLocalHosts ClusterLocalHosts

	// sidecars for each namespace
	sidecarsByNamespace map[string][]*SidecarScope

	// envoy filters for each namespace including global config namespace
	envoyFiltersByNamespace map[string][]*EnvoyFilterWrapper

	// AuthnPolicies contains Authn policies by namespace.
	AuthnPolicies *AuthenticationPolicies `json:"-"`

	// AuthzPolicies stores the existing authorization policies in the cluster. Could be nil if there
	// are no authorization policies in the cluster.
	AuthzPolicies *AuthorizationPolicies `json:"-"`

	// The following data is either a global index or used in the inbound path.
	// Namespace specific views do not apply here.

	// Mesh configuration for the mesh.
	Mesh *meshconfig.MeshConfig `json:"-"`

	// Discovery interface for listing services and instances.
	ServiceDiscovery `json:"-"`

	// Config interface for listing routing rules
	IstioConfigStore `json:"-"`

	// PushVersion describes the push version this push context was computed for
	PushVersion string

	// LedgerVersion is the version of the configuration ledger
	LedgerVersion string

	// JwtKeyResolver holds a reference to the JWT key resolver instance.
	JwtKeyResolver *JwksResolver

	// cache gateways addresses for each network
	// this is mainly used for kubernetes multi-cluster scenario
	networksMu      sync.RWMutex
	networkGateways map[string][]*Gateway

	initDone        atomic.Bool
	initializeMutex sync.Mutex
}

// Gateway is the gateway of a network
type Gateway struct {
	// gateway ip address
	Addr string
	// gateway port
	Port uint32
}

type processedDestRules struct {
	// List of dest rule hosts. We match with the most specific host first
	hosts []host.Name
	// Map of dest rule hosts.
	hostsMap map[host.Name]struct{}
	// Map of dest rule host to the list of namespaces to which this destination rule has been exported to
	exportTo map[host.Name]map[visibility.Instance]bool
	// Map of dest rule host and the merged destination rules for that host
	destRule map[host.Name]*config.Config
}

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
	EDSUpdate(shard, hostname string, namespace string, entry []*IstioEndpoint)

	// EDSCacheUpdate is called when the list of endpoints or labels in a Service is changed.
	// For each cluster and hostname, the full list of active endpoints (including empty list)
	// must be sent. The shard name is used as a key - current implementation is using the
	// registry name.
	// Note: the difference with `EDSUpdate` is that it only update the cache rather than requesting a push
	EDSCacheUpdate(shard, hostname string, namespace string, entry []*IstioEndpoint)

	// SvcUpdate is called when a service definition is updated/deleted.
	SvcUpdate(shard, hostname string, namespace string, event Event)

	// ConfigUpdate is called to notify the XDS server of config updates and request a push.
	// The requests may be collapsed and throttled.
	ConfigUpdate(req *PushRequest)

	// ProxyUpdate is called to notify the XDS server to send a push to the specified proxy.
	// The requests may be collapsed and throttled.
	ProxyUpdate(clusterID, ip string)
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
	ConfigsUpdated map[ConfigKey]struct{}

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
	Reason []TriggerReason
}

type TriggerReason string

const (
	// Describes a push triggered by an Endpoint change
	EndpointUpdate TriggerReason = "endpoint"
	// Describes a push triggered by a config (generally and Istio CRD) change.
	ConfigUpdate TriggerReason = "config"
	// Describes a push triggered by a Service change
	ServiceUpdate TriggerReason = "service"
	// Describes a push triggered by a change to an individual proxy (such as label change)
	ProxyUpdate TriggerReason = "proxy"
	// Describes a push triggered by a change to global config, such as mesh config
	GlobalUpdate TriggerReason = "global"
	// Describes a push triggered by an unknown reason
	UnknownTrigger TriggerReason = "unknown"
	// Describes a push triggered for debugging
	DebugTrigger TriggerReason = "debug"
	// Describes a push triggered for a Secret change
	SecretTrigger TriggerReason = "secret"
	// Describes a push triggered for Networks change
	NetworksTrigger TriggerReason = "networks"
	// Desribes a push triggered based on proxy request
	ProxyRequest TriggerReason = "proxyrequest"
)

// Merge two update requests together
func (pr *PushRequest) Merge(other *PushRequest) *PushRequest {
	if pr == nil {
		return other
	}
	if other == nil {
		return pr
	}

	reason := make([]TriggerReason, 0, len(pr.Reason)+len(other.Reason))
	reason = append(reason, pr.Reason...)
	reason = append(reason, other.Reason...)
	merged := &PushRequest{
		// Keep the first (older) start time
		Start: pr.Start,

		// If either is full we need a full push
		Full: pr.Full || other.Full,

		// The other push context is presumed to be later and more up to date
		Push: other.Push,

		// Merge the two reasons. Note that we shouldn't deduplicate here, or we would under count
		Reason: reason,
	}

	// Do not merge when any one is empty
	if len(pr.ConfigsUpdated) > 0 && len(other.ConfigsUpdated) > 0 {
		merged.ConfigsUpdated = make(map[ConfigKey]struct{}, len(pr.ConfigsUpdated)+len(other.ConfigsUpdated))
		for conf := range pr.ConfigsUpdated {
			merged.ConfigsUpdated[conf] = struct{}{}
		}
		for conf := range other.ConfigsUpdated {
			merged.ConfigsUpdated[conf] = struct{}{}
		}
	}

	return merged
}

func (pr *PushRequest) PushReason() string {
	if len(pr.Reason) == 1 && pr.Reason[0] == ProxyRequest {
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
	// Updated by GetProxyServiceInstances
	ProxyStatusNoService = monitoring.NewGauge(
		"pilot_no_ip",
		"Pods not found in the endpoint table, possibly invalid.",
	)

	// ProxyStatusEndpointNotReady represents proxies found not be ready.
	// Updated by GetProxyServiceInstances. Normal condition when starting
	// an app with readiness, error if it doesn't change to 0.
	ProxyStatusEndpointNotReady = monitoring.NewGauge(
		"pilot_endpoint_not_ready",
		"Endpoint found in unready state.",
	)

	// ProxyStatusConflictOutboundListenerTCPOverHTTP metric tracks number of
	// wildcard TCP listeners that conflicted with existing wildcard HTTP listener on same port
	ProxyStatusConflictOutboundListenerTCPOverHTTP = monitoring.NewGauge(
		"pilot_conflict_outbound_listener_tcp_over_current_http",
		"Number of conflicting wildcard tcp listeners with current wildcard http listener.",
	)

	// ProxyStatusConflictOutboundListenerTCPOverTCP metric tracks number of
	// TCP listeners that conflicted with existing TCP listeners on same port
	ProxyStatusConflictOutboundListenerTCPOverTCP = monitoring.NewGauge(
		"pilot_conflict_outbound_listener_tcp_over_current_tcp",
		"Number of conflicting tcp listeners with current tcp listener.",
	)

	// ProxyStatusConflictOutboundListenerHTTPOverTCP metric tracks number of
	// wildcard HTTP listeners that conflicted with existing wildcard TCP listener on same port
	ProxyStatusConflictOutboundListenerHTTPOverTCP = monitoring.NewGauge(
		"pilot_conflict_outbound_listener_http_over_current_tcp",
		"Number of conflicting wildcard http listeners with current wildcard tcp listener.",
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
		EndpointNoPod,
		ProxyStatusNoService,
		ProxyStatusEndpointNotReady,
		ProxyStatusConflictOutboundListenerTCPOverHTTP,
		ProxyStatusConflictOutboundListenerTCPOverTCP,
		ProxyStatusConflictOutboundListenerHTTPOverTCP,
		ProxyStatusConflictInboundListener,
		DuplicatedClusters,
		ProxyStatusClusterNoInstances,
		DuplicatedDomains,
		DuplicatedSubsets,
	}
)

func init() {
	for _, m := range metrics {
		monitoring.MustRegister(m)
	}
	monitoring.MustRegister(totalVirtualServices)
}

// NewPushContext creates a new PushContext structure to track push status.
func NewPushContext() *PushContext {
	// TODO: detect push in progress, don't update status if set
	return &PushContext{
		ServiceIndex:            newServiceIndex(),
		virtualServiceIndex:     newVirtualServiceIndex(),
		destinationRuleIndex:    newDestinationRuleIndex(),
		sidecarsByNamespace:     map[string][]*SidecarScope{},
		envoyFiltersByNamespace: map[string][]*EnvoyFilterWrapper{},
		gatewayIndex:            newGatewayIndex(),
		ProxyStatus:             map[string]map[string]ProxyPushStatus{},
		ServiceAccounts:         map[host.Name]map[int][]string{},
	}
}

// AddPublicServices adds the services to context public services - mainly used in tests.
func (ps *PushContext) AddPublicServices(services []*Service) {
	ps.ServiceIndex.public = append(ps.ServiceIndex.public, services...)
}

// AddServiceInstances adds instances to the context service instances - mainly used in tests.
func (ps *PushContext) AddServiceInstances(service *Service, instances map[int][]*ServiceInstance) {
	for port, inst := range instances {
		if _, exists := ps.ServiceIndex.instancesByPort[service]; !exists {
			ps.ServiceIndex.instancesByPort[service] = make(map[int][]*ServiceInstance)
		}
		ps.ServiceIndex.instancesByPort[service][port] = append(ps.ServiceIndex.instancesByPort[service][port], inst...)
	}
}

// JSON implements json.Marshaller, with a lock.
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

func virtualServiceDestinations(v *networking.VirtualService) []*networking.Destination {
	if v == nil {
		return nil
	}

	var ds []*networking.Destination

	for _, h := range v.Http {
		for _, r := range h.Route {
			if r.Destination != nil {
				ds = append(ds, r.Destination)
			}
		}
		if h.Mirror != nil {
			ds = append(ds, h.Mirror)
		}
	}
	for _, t := range v.Tcp {
		for _, r := range t.Route {
			if r.Destination != nil {
				ds = append(ds, r.Destination)
			}
		}
	}
	for _, t := range v.Tls {
		for _, r := range t.Route {
			if r.Destination != nil {
				ds = append(ds, r.Destination)
			}
		}
	}

	return ds
}

// GatewayServices returns the set of services which are referred from the proxy gateways.
func (ps *PushContext) GatewayServices(proxy *Proxy) []*Service {
	svcs := ps.Services(proxy)
	// host set.
	hostsFromGateways := map[string]struct{}{}

	// MergedGateway will be nil when there are no configs in the
	// system during initial installation.
	if proxy.MergedGateway == nil {
		return nil
	}

	for _, gw := range proxy.MergedGateway.GatewayNameForServer {
		for _, vsConfig := range ps.VirtualServicesForGateway(proxy, gw) {
			vs, ok := vsConfig.Spec.(*networking.VirtualService)
			if !ok { // should never happen
				log.Errorf("Failed in getting a virtual service: %v", vsConfig.Labels)
				return svcs
			}

			for _, d := range virtualServiceDestinations(vs) {
				hostsFromGateways[d.Host] = struct{}{}
			}
		}
	}

	log.Debugf("GatewayServices: gateway %v is exposing these hosts:%v", proxy.ID, hostsFromGateways)

	gwSvcs := make([]*Service, 0, len(svcs))

	for _, s := range svcs {
		svcHost := string(s.Hostname)

		if _, ok := hostsFromGateways[svcHost]; ok {
			gwSvcs = append(gwSvcs, s)
		}
	}

	log.Debugf("GatewayServices:: gateways len(services)=%d, len(filtered)=%d", len(svcs), len(gwSvcs))

	return gwSvcs
}

// Services returns the list of services that are visible to a Proxy in a given config namespace
func (ps *PushContext) Services(proxy *Proxy) []*Service {
	// If proxy has a sidecar scope that is user supplied, then get the services from the sidecar scope
	// sidecarScope.config is nil if there is no sidecar scope for the namespace
	if proxy != nil && proxy.SidecarScope != nil && proxy.Type == SidecarProxy {
		return proxy.SidecarScope.Services()
	}

	out := make([]*Service, 0)

	// First add private services and explicitly exportedTo services
	if proxy == nil {
		for _, privateServices := range ps.ServiceIndex.privateByNamespace {
			out = append(out, privateServices...)
		}
	} else {
		out = append(out, ps.ServiceIndex.privateByNamespace[proxy.ConfigNamespace]...)
		out = append(out, ps.ServiceIndex.exportedToNamespace[proxy.ConfigNamespace]...)
	}

	// Second add public services
	out = append(out, ps.ServiceIndex.public...)

	return out
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
	if len(service.Attributes.ExportTo) == 0 {
		if ps.exportToDefaults.service[visibility.Private] {
			return ns == namespace
		} else if ps.exportToDefaults.service[visibility.Public] {
			return true
		}
	}

	return service.Attributes.ExportTo[visibility.Public] ||
		(service.Attributes.ExportTo[visibility.Private] && ns == namespace) ||
		service.Attributes.ExportTo[visibility.Instance(namespace)]
}

// VirtualServicesForGateway lists all virtual services bound to the specified gateways
// This replaces store.VirtualServices. Used only by the gateways
// Sidecars use the egressListener.VirtualServices().
func (ps *PushContext) VirtualServicesForGateway(proxy *Proxy, gateway string) []config.Config {
	res := ps.virtualServiceIndex.privateByNamespaceAndGateway[proxy.ConfigNamespace][gateway]
	res = append(res, ps.virtualServiceIndex.exportedToNamespaceByGateway[proxy.ConfigNamespace][gateway]...)
	res = append(res, ps.virtualServiceIndex.publicByGateway[gateway]...)
	return res
}

// DelegateVirtualServicesConfigKey lists all the delegate virtual services configkeys associated with the provided virtual services
func (ps *PushContext) DelegateVirtualServicesConfigKey(vses []config.Config) []ConfigKey {
	var out []ConfigKey
	for _, vs := range vses {
		out = append(out, ps.virtualServiceIndex.delegates[ConfigKey{Kind: gvk.VirtualService, Namespace: vs.Namespace, Name: vs.Name}]...)
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
func (ps *PushContext) getSidecarScope(proxy *Proxy, workloadLabels labels.Collection) *SidecarScope {
	// Find the most specific matching sidecar config from the proxy's
	// config namespace If none found, construct a sidecarConfig on the fly
	// that allows the sidecar to talk to any namespace (the default
	// behavior in the absence of sidecars).
	if sidecars, ok := ps.sidecarsByNamespace[proxy.ConfigNamespace]; ok {
		// TODO: logic to merge multiple sidecar resources
		// Currently we assume that there will be only one sidecar config for a namespace.
		for _, wrapper := range sidecars {
			if wrapper.Sidecar != nil {
				sidecar := wrapper.Sidecar
				// if there is no workload selector, the config applies to all workloads
				// if there is a workload selector, check for matching workload labels
				if sidecar.GetWorkloadSelector() != nil {
					workloadSelector := labels.Instance(sidecar.GetWorkloadSelector().GetLabels())
					// exclude workload selector that not match
					if !workloadLabels.IsSupersetOf(workloadSelector) {
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

	return DefaultSidecarScopeForNamespace(ps, proxy.ConfigNamespace)
}

// DestinationRule returns a destination rule for a service name in a given domain.
func (ps *PushContext) DestinationRule(proxy *Proxy, service *Service) *config.Config {
	if service == nil {
		return nil
	}

	// If proxy has a sidecar scope that is user supplied, then get the destination rules from the sidecar scope
	// sidecarScope.config is nil if there is no sidecar scope for the namespace
	if proxy.SidecarScope != nil && proxy.Type == SidecarProxy {
		// If there is a sidecar scope for this proxy, return the destination rule
		// from the sidecar scope.
		return proxy.SidecarScope.DestinationRule(service.Hostname)
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
	if proxy.ConfigNamespace != ps.Mesh.RootNamespace {
		// search through the DestinationRules in proxy's namespace first
		if ps.destinationRuleIndex.namespaceLocal[proxy.ConfigNamespace] != nil {
			if hostname, ok := MostSpecificHostMatch(service.Hostname,
				ps.destinationRuleIndex.namespaceLocal[proxy.ConfigNamespace].hostsMap,
				ps.destinationRuleIndex.namespaceLocal[proxy.ConfigNamespace].hosts,
			); ok {
				return ps.destinationRuleIndex.namespaceLocal[proxy.ConfigNamespace].destRule[hostname]
			}
		}
	} else {
		// If this is a namespace local DR in the same namespace, this must be meant for this proxy, so we do not
		// need to worry about overriding other DRs with *.local type rules here. If we ignore this, then exportTo=. in
		// root namespace would always be ignored
		if hostname, ok := MostSpecificHostMatch(service.Hostname,
			ps.destinationRuleIndex.rootNamespaceLocal.hostsMap,
			ps.destinationRuleIndex.rootNamespaceLocal.hosts,
		); ok {
			return ps.destinationRuleIndex.rootNamespaceLocal.destRule[hostname]
		}
	}

	// 2. select destination rule from service namespace
	svcNs := service.Attributes.Namespace

	// This can happen when finding the subset labels for a proxy in root namespace.
	// Because based on a pure cluster's fqdn, we do not know the service and
	// construct a fake service without setting Attributes at all.
	if svcNs == "" {
		for _, svc := range ps.Services(proxy) {
			if service.Hostname == svc.Hostname && svc.Attributes.Namespace != "" {
				svcNs = svc.Attributes.Namespace
				break
			}
		}
	}

	// 3. if no private/public rule matched in the calling proxy's namespace,
	// check the target service's namespace for exported rules
	if svcNs != "" {
		if out := ps.getExportedDestinationRuleFromNamespace(svcNs, service.Hostname, proxy.ConfigNamespace); out != nil {
			return out
		}
	}

	// 4. if no public/private rule in calling proxy's namespace matched, and no public rule in the
	// target service's namespace matched, search for any exported destination rule in the config root namespace
	if out := ps.getExportedDestinationRuleFromNamespace(ps.Mesh.RootNamespace, service.Hostname, proxy.ConfigNamespace); out != nil {
		return out
	}

	// 5. service DestinationRules were merged in SetDestinationRules, return mesh/namespace rules if present
	if features.EnableDestinationRuleInheritance {
		// return namespace rule if present
		if out := ps.destinationRuleIndex.inheritedByNamespace[proxy.ConfigNamespace]; out != nil {
			return out
		}
		// return mesh rule
		if out := ps.destinationRuleIndex.inheritedByNamespace[ps.Mesh.RootNamespace]; out != nil {
			return out
		}
	}

	return nil
}

func (ps *PushContext) getExportedDestinationRuleFromNamespace(owningNamespace string, hostname host.Name, clientNamespace string) *config.Config {
	if ps.destinationRuleIndex.exportedByNamespace[owningNamespace] != nil {
		if specificHostname, ok := MostSpecificHostMatch(hostname,
			ps.destinationRuleIndex.exportedByNamespace[owningNamespace].hostsMap,
			ps.destinationRuleIndex.exportedByNamespace[owningNamespace].hosts,
		); ok {
			// Check if the dest rule for this host is actually exported to the proxy's (client) namespace
			exportToMap := ps.destinationRuleIndex.exportedByNamespace[owningNamespace].exportTo[specificHostname]
			if len(exportToMap) == 0 || exportToMap[visibility.Public] || exportToMap[visibility.Instance(clientNamespace)] {
				if features.EnableDestinationRuleInheritance {
					var parent *config.Config
					// client inherits global DR from its own namespace, not from the exported DR's owning namespace
					// grab the client namespace DR or mesh if none exists
					if parent = ps.destinationRuleIndex.inheritedByNamespace[clientNamespace]; parent == nil {
						parent = ps.destinationRuleIndex.inheritedByNamespace[ps.Mesh.RootNamespace]
					}
					if child := ps.destinationRuleIndex.exportedByNamespace[owningNamespace].destRule[specificHostname]; child != nil {
						return ps.inheritDestinationRule(parent, child)
					}
					return nil
				}
				return ps.destinationRuleIndex.exportedByNamespace[owningNamespace].destRule[specificHostname]
			}
		}
	}
	return nil
}

// IsClusterLocal indicates whether the endpoints for the service should only be accessible to clients
// within the cluster.
func (ps *PushContext) IsClusterLocal(service *Service) bool {
	return ps.clusterLocalHosts.IsClusterLocal(service.Hostname)
}

// SubsetToLabels returns the labels associated with a subset of a given service.
func (ps *PushContext) SubsetToLabels(proxy *Proxy, subsetName string, hostname host.Name) labels.Collection {
	// empty subset
	if subsetName == "" {
		return nil
	}

	cfg := ps.DestinationRule(proxy, &Service{Hostname: hostname})
	if cfg == nil {
		return nil
	}

	rule := cfg.Spec.(*networking.DestinationRule)
	for _, subset := range rule.Subsets {
		if subset.Name == subsetName {
			if len(subset.Labels) == 0 {
				return nil
			}
			return []labels.Instance{subset.Labels}
		}
	}

	return nil
}

// InitContext will initialize the data structures used for code generation.
// This should be called before starting the push, from the thread creating
// the push context.
func (ps *PushContext) InitContext(env *Environment, oldPushContext *PushContext, pushReq *PushRequest) error {
	// Acquire a lock to ensure we don't concurrently initialize the same PushContext.
	// If this does happen, one thread will block then exit early from initDone=true
	ps.initializeMutex.Lock()
	defer ps.initializeMutex.Unlock()
	if ps.initDone.Load() {
		return nil
	}

	ps.Mesh = env.Mesh()
	ps.ServiceDiscovery = env.ServiceDiscovery
	ps.IstioConfigStore = env.IstioConfigStore
	ps.LedgerVersion = env.Version()

	// Must be initialized first
	// as initServiceRegistry/VirtualServices/Destrules
	// use the default export map
	ps.initDefaultExportMaps()

	// create new or incremental update
	if pushReq == nil || oldPushContext == nil || !oldPushContext.initDone.Load() || len(pushReq.ConfigsUpdated) == 0 {
		if err := ps.createNewContext(env); err != nil {
			return err
		}
	} else {
		if err := ps.updateContext(env, oldPushContext, pushReq); err != nil {
			return err
		}
	}

	// TODO: only do this when meshnetworks or gateway service changed
	ps.initMeshNetworks(env.Networks())

	ps.clusterLocalHosts = env.ClusterLocal().GetClusterLocalHosts()

	ps.initDone.Store(true)
	return nil
}

func (ps *PushContext) createNewContext(env *Environment) error {
	if err := ps.initServiceRegistry(env); err != nil {
		return err
	}

	if err := ps.initVirtualServices(env); err != nil {
		return err
	}

	if err := ps.initDestinationRules(env); err != nil {
		return err
	}

	if err := ps.initAuthnPolicies(env); err != nil {
		return err
	}

	if err := ps.initAuthorizationPolicies(env); err != nil {
		authzLog.Errorf("failed to initialize authorization policies: %v", err)
		return err
	}

	if err := ps.initEnvoyFilters(env); err != nil {
		return err
	}

	if err := ps.initGateways(env); err != nil {
		return err
	}

	// Must be initialized in the end
	if err := ps.initSidecarScopes(env); err != nil {
		return err
	}
	return nil
}

func (ps *PushContext) updateContext(
	env *Environment,
	oldPushContext *PushContext,
	pushReq *PushRequest) error {
	var servicesChanged, virtualServicesChanged, destinationRulesChanged, gatewayChanged,
		authnChanged, authzChanged, envoyFiltersChanged, sidecarsChanged bool

	for conf := range pushReq.ConfigsUpdated {
		switch conf.Kind {
		case gvk.ServiceEntry:
			servicesChanged = true
		case gvk.DestinationRule:
			destinationRulesChanged = true
		case gvk.VirtualService:
			virtualServicesChanged = true
		case gvk.Gateway:
			gatewayChanged = true
		case gvk.Sidecar:
			sidecarsChanged = true
		case gvk.EnvoyFilter:
			envoyFiltersChanged = true
		case gvk.AuthorizationPolicy:
			authzChanged = true
		case gvk.RequestAuthentication,
			gvk.PeerAuthentication:
			authnChanged = true
		case gvk.HTTPRoute, gvk.TCPRoute, gvk.GatewayClass, gvk.ServiceApisGateway, gvk.TLSRoute:
			virtualServicesChanged = true
			gatewayChanged = true
		}
	}

	if servicesChanged {
		// Services have changed. initialize service registry
		if err := ps.initServiceRegistry(env); err != nil {
			return err
		}
	} else {
		// make sure we copy over things that would be generated in initServiceRegistry
		ps.ServiceIndex = oldPushContext.ServiceIndex
		ps.ServiceAccounts = oldPushContext.ServiceAccounts
	}

	if virtualServicesChanged {
		if err := ps.initVirtualServices(env); err != nil {
			return err
		}
	} else {
		ps.virtualServiceIndex = oldPushContext.virtualServiceIndex
	}

	if destinationRulesChanged {
		if err := ps.initDestinationRules(env); err != nil {
			return err
		}
	} else {
		ps.destinationRuleIndex = oldPushContext.destinationRuleIndex
	}

	if authnChanged {
		if err := ps.initAuthnPolicies(env); err != nil {
			return err
		}
	} else {
		ps.AuthnPolicies = oldPushContext.AuthnPolicies
	}

	if authzChanged {
		if err := ps.initAuthorizationPolicies(env); err != nil {
			authzLog.Errorf("failed to initialize authorization policies: %v", err)
			return err
		}
	} else {
		ps.AuthzPolicies = oldPushContext.AuthzPolicies
	}

	if envoyFiltersChanged {
		if err := ps.initEnvoyFilters(env); err != nil {
			return err
		}
	} else {
		ps.envoyFiltersByNamespace = oldPushContext.envoyFiltersByNamespace
	}

	if gatewayChanged {
		if err := ps.initGateways(env); err != nil {
			return err
		}
	} else {
		ps.gatewayIndex = oldPushContext.gatewayIndex
	}

	// Must be initialized in the end
	// Sidecars need to be updated if services, virtual services, destination rules, or the sidecar configs change
	if servicesChanged || virtualServicesChanged || destinationRulesChanged || sidecarsChanged {
		if err := ps.initSidecarScopes(env); err != nil {
			return err
		}
	} else {
		ps.sidecarsByNamespace = oldPushContext.sidecarsByNamespace
	}

	return nil
}

// Caches list of services in the registry, and creates a map
// of hostname to service
func (ps *PushContext) initServiceRegistry(env *Environment) error {
	services, err := env.Services()
	if err != nil {
		return err
	}
	// Sort the services in order of creation.
	allServices := sortServicesByCreationTime(services)
	for _, s := range allServices {
		// Precache instances
		for _, port := range s.Ports {
			if _, ok := ps.ServiceIndex.instancesByPort[s]; !ok {
				ps.ServiceIndex.instancesByPort[s] = make(map[int][]*ServiceInstance)
			}
			instances := make([]*ServiceInstance, 0)
			instances = append(instances, ps.InstancesByPort(s, port.Port, nil)...)
			ps.ServiceIndex.instancesByPort[s][port.Port] = instances
		}

		if _, f := ps.ServiceIndex.HostnameAndNamespace[s.Hostname]; !f {
			ps.ServiceIndex.HostnameAndNamespace[s.Hostname] = map[string]*Service{}
		}
		ps.ServiceIndex.HostnameAndNamespace[s.Hostname][s.Attributes.Namespace] = s

		ns := s.Attributes.Namespace
		if len(s.Attributes.ExportTo) == 0 {
			if ps.exportToDefaults.service[visibility.Private] {
				ps.ServiceIndex.privateByNamespace[ns] = append(ps.ServiceIndex.privateByNamespace[ns], s)
			} else if ps.exportToDefaults.service[visibility.Public] {
				ps.ServiceIndex.public = append(ps.ServiceIndex.public, s)
			}
		} else {
			// if service has exportTo ~ - i.e. not visible to anyone, ignore all exportTos
			// if service has exportTo *, make public and ignore all other exportTos
			// if service has exportTo ., replace with current namespace
			if s.Attributes.ExportTo[visibility.Public] {
				ps.ServiceIndex.public = append(ps.ServiceIndex.public, s)
				continue
			} else if s.Attributes.ExportTo[visibility.None] {
				continue
			} else {
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
	}

	ps.initServiceAccounts(env, allServices)

	return nil
}

// sortServicesByCreationTime sorts the list of services in ascending order by their creation time (if available).
func sortServicesByCreationTime(services []*Service) []*Service {
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].CreationTime.Before(services[j].CreationTime)
	})
	return services
}

// Caches list of service accounts in the registry
func (ps *PushContext) initServiceAccounts(env *Environment, services []*Service) {
	for _, svc := range services {
		if ps.ServiceAccounts[svc.Hostname] == nil {
			ps.ServiceAccounts[svc.Hostname] = map[int][]string{}
		}
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			ps.ServiceAccounts[svc.Hostname][port.Port] = env.GetIstioServiceAccounts(svc, []int{port.Port})
		}
	}
}

// Caches list of authentication policies
func (ps *PushContext) initAuthnPolicies(env *Environment) error {
	// Init beta policy.
	var err error
	ps.AuthnPolicies, err = initAuthenticationPolicies(env)
	return err
}

// Caches list of virtual services
func (ps *PushContext) initVirtualServices(env *Environment) error {
	ps.virtualServiceIndex.exportedToNamespaceByGateway = map[string]map[string][]config.Config{}
	ps.virtualServiceIndex.privateByNamespaceAndGateway = map[string]map[string][]config.Config{}
	ps.virtualServiceIndex.publicByGateway = map[string][]config.Config{}

	virtualServices, err := env.List(gvk.VirtualService, NamespaceAll)
	if err != nil {
		return err
	}

	// values returned from ConfigStore.List are immutable.
	// Therefore, we make a copy
	vservices := make([]config.Config, len(virtualServices))

	for i := range vservices {
		vservices[i] = virtualServices[i].DeepCopy()
	}

	totalVirtualServices.Record(float64(len(virtualServices)))

	// TODO(rshriram): parse each virtual service and maintain a map of the
	// virtualservice name, the list of registry hosts in the VS and non
	// registry DNS names in the VS.  This should cut down processing in
	// the RDS code. See separateVSHostsAndServices in route/route.go
	sortConfigByCreationTime(vservices)

	// convert all shortnames in virtual services into FQDNs
	for _, r := range vservices {
		resolveVirtualServiceShortnames(r.Spec.(*networking.VirtualService), r.Meta)
	}

	vservices, ps.virtualServiceIndex.delegates = mergeVirtualServicesIfNeeded(vservices, ps.exportToDefaults.virtualService)

	for _, virtualService := range vservices {
		ns := virtualService.Namespace
		rule := virtualService.Spec.(*networking.VirtualService)
		gwNames := getGatewayNames(rule, virtualService.Meta)
		if len(rule.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			// We only honor ., *
			if ps.exportToDefaults.virtualService[visibility.Private] {
				if _, f := ps.virtualServiceIndex.privateByNamespaceAndGateway[ns]; !f {
					ps.virtualServiceIndex.privateByNamespaceAndGateway[ns] = map[string][]config.Config{}
				}
				// add to local namespace only
				for _, gw := range gwNames {
					ps.virtualServiceIndex.privateByNamespaceAndGateway[ns][gw] = append(ps.virtualServiceIndex.privateByNamespaceAndGateway[ns][gw], virtualService)
				}
			} else if ps.exportToDefaults.virtualService[visibility.Public] {
				for _, gw := range gwNames {
					ps.virtualServiceIndex.publicByGateway[gw] = append(ps.virtualServiceIndex.publicByGateway[gw], virtualService)
				}
			}
		} else {
			exportToMap := make(map[visibility.Instance]bool)
			for _, e := range rule.ExportTo {
				exportToMap[visibility.Instance(e)] = true
			}
			// if vs has exportTo ~ - i.e. not visible to anyone, ignore all exportTos
			// if vs has exportTo *, make public and ignore all other exportTos
			// if vs has exportTo ., replace with current namespace
			if exportToMap[visibility.Public] {
				for _, gw := range gwNames {
					ps.virtualServiceIndex.publicByGateway[gw] = append(ps.virtualServiceIndex.publicByGateway[gw], virtualService)
				}
				continue
			} else if exportToMap[visibility.None] {
				// not possible
				continue
			} else {
				// . or other namespaces
				for exportTo := range exportToMap {
					if exportTo == visibility.Private || string(exportTo) == ns {
						if _, f := ps.virtualServiceIndex.privateByNamespaceAndGateway[ns]; !f {
							ps.virtualServiceIndex.privateByNamespaceAndGateway[ns] = map[string][]config.Config{}
						}
						// add to local namespace only
						for _, gw := range gwNames {
							ps.virtualServiceIndex.privateByNamespaceAndGateway[ns][gw] = append(ps.virtualServiceIndex.privateByNamespaceAndGateway[ns][gw], virtualService)
						}
					} else {
						if _, f := ps.virtualServiceIndex.exportedToNamespaceByGateway[string(exportTo)]; !f {
							ps.virtualServiceIndex.exportedToNamespaceByGateway[string(exportTo)] = map[string][]config.Config{}
						}
						// add to local namespace only
						for _, gw := range gwNames {
							ps.virtualServiceIndex.exportedToNamespaceByGateway[string(exportTo)][gw] =
								append(ps.virtualServiceIndex.exportedToNamespaceByGateway[string(exportTo)][gw], virtualService)
						}
					}
				}
			}
		}
	}

	return nil
}

var meshGateways = []string{constants.IstioMeshGateway}

func getGatewayNames(vs *networking.VirtualService, meta config.Meta) []string {
	if len(vs.Gateways) == 0 {
		return meshGateways
	}
	res := make([]string, 0, len(vs.Gateways))
	for _, g := range vs.Gateways {
		if g == constants.IstioMeshGateway {
			res = append(res, constants.IstioMeshGateway)
		} else {
			name := resolveGatewayName(g, meta)
			res = append(res, name)
		}
	}
	return res
}

func (ps *PushContext) initDefaultExportMaps() {
	ps.exportToDefaults.destinationRule = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultDestinationRuleExportTo != nil {
		for _, e := range ps.Mesh.DefaultDestinationRuleExportTo {
			ps.exportToDefaults.destinationRule[visibility.Instance(e)] = true
		}
	} else {
		// default to *
		ps.exportToDefaults.destinationRule[visibility.Public] = true
	}

	ps.exportToDefaults.service = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultServiceExportTo {
			ps.exportToDefaults.service[visibility.Instance(e)] = true
		}
	} else {
		ps.exportToDefaults.service[visibility.Public] = true
	}

	ps.exportToDefaults.virtualService = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultVirtualServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultVirtualServiceExportTo {
			ps.exportToDefaults.virtualService[visibility.Instance(e)] = true
		}
	} else {
		ps.exportToDefaults.virtualService[visibility.Public] = true
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
func (ps *PushContext) initSidecarScopes(env *Environment) error {
	sidecarConfigs, err := env.List(gvk.Sidecar, NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(sidecarConfigs)

	sidecarConfigWithSelector := make([]config.Config, 0)
	sidecarConfigWithoutSelector := make([]config.Config, 0)
	sidecarsWithoutSelectorByNamespace := make(map[string]struct{})
	for _, sidecarConfig := range sidecarConfigs {
		sidecar := sidecarConfig.Spec.(*networking.Sidecar)
		if sidecar.WorkloadSelector != nil {
			sidecarConfigWithSelector = append(sidecarConfigWithSelector, sidecarConfig)
		} else {
			sidecarsWithoutSelectorByNamespace[sidecarConfig.Namespace] = struct{}{}
			sidecarConfigWithoutSelector = append(sidecarConfigWithoutSelector, sidecarConfig)
		}
	}

	sidecarNum := len(sidecarConfigs)
	sidecarConfigs = make([]config.Config, 0, sidecarNum)
	sidecarConfigs = append(sidecarConfigs, sidecarConfigWithSelector...)
	sidecarConfigs = append(sidecarConfigs, sidecarConfigWithoutSelector...)

	ps.sidecarsByNamespace = make(map[string][]*SidecarScope, sidecarNum)
	for _, sidecarConfig := range sidecarConfigs {
		sidecarConfig := sidecarConfig
		ps.sidecarsByNamespace[sidecarConfig.Namespace] = append(ps.sidecarsByNamespace[sidecarConfig.Namespace],
			ConvertToSidecarScope(ps, &sidecarConfig, sidecarConfig.Namespace))
	}

	// Hold reference root namespace's sidecar config
	// Root namespace can have only one sidecar config object
	// Currently we expect that it has no workloadSelectors
	var rootNSConfig *config.Config
	if ps.Mesh.RootNamespace != "" {
		for _, sidecarConfig := range sidecarConfigs {
			if sidecarConfig.Namespace == ps.Mesh.RootNamespace &&
				sidecarConfig.Spec.(*networking.Sidecar).WorkloadSelector == nil {
				rootNSConfig = &sidecarConfig
				break
			}
		}
	}

	// build sidecar scopes for namespaces that do not have a non-workloadSelector sidecar CRD object.
	// Derive the sidecar scope from the root namespace's sidecar object if present. Else fallback
	// to the default Istio behavior mimicked by the DefaultSidecarScopeForNamespace function.
	namespaces := sets.NewSet()
	for _, nsMap := range ps.ServiceIndex.HostnameAndNamespace {
		for ns := range nsMap {
			namespaces.Insert(ns)
		}
	}
	for ns := range namespaces {
		if _, exist := sidecarsWithoutSelectorByNamespace[ns]; !exist {
			ps.sidecarsByNamespace[ns] = append(ps.sidecarsByNamespace[ns], ConvertToSidecarScope(ps, rootNSConfig, ns))
		}
	}

	return nil
}

// Split out of DestinationRule expensive conversions - once per push.
func (ps *PushContext) initDestinationRules(env *Environment) error {
	configs, err := env.List(gvk.DestinationRule, NamespaceAll)
	if err != nil {
		return err
	}

	// values returned from ConfigStore.List are immutable.
	// Therefore, we make a copy
	destRules := make([]config.Config, len(configs))
	for i := range destRules {
		destRules[i] = configs[i].DeepCopy()
	}

	ps.SetDestinationRules(destRules)
	return nil
}

func newProcessedDestRules() *processedDestRules {
	return &processedDestRules{
		hosts:    make([]host.Name, 0),
		exportTo: map[host.Name]map[visibility.Instance]bool{},
		destRule: map[host.Name]*config.Config{},
	}
}

// SetDestinationRules is updates internal structures using a set of configs.
// Split out of DestinationRule expensive conversions, computed once per push.
// This also allows tests to inject a config without having the mock.
// This will not work properly for Sidecars, which will precompute their destination rules on init
func (ps *PushContext) SetDestinationRules(configs []config.Config) {
	// Sort by time first. So if two destination rule have top level traffic policies
	// we take the first one.
	sortConfigByCreationTime(configs)
	namespaceLocalDestRules := make(map[string]*processedDestRules)
	exportedDestRulesByNamespace := make(map[string]*processedDestRules)
	rootNamespaceLocalDestRules := newProcessedDestRules()
	inheritedConfigs := make(map[string]*config.Config)

	for i := range configs {
		rule := configs[i].Spec.(*networking.DestinationRule)

		if features.EnableDestinationRuleInheritance && rule.Host == "" {
			if t, ok := inheritedConfigs[configs[i].Namespace]; ok {
				log.Warnf(
					"Namespace/mesh-level DestinationRule is already defined for %q at time %v. Ignore %q which was created at time %v",
					configs[i].Namespace, t.CreationTimestamp, configs[i].Name, configs[i].CreationTimestamp)
				continue
			}
			inheritedConfigs[configs[i].Namespace] = &configs[i]
		}

		rule.Host = string(ResolveShortnameToFQDN(rule.Host, configs[i].Meta))
		exportToMap := make(map[visibility.Instance]bool)
		for _, e := range rule.ExportTo {
			exportToMap[visibility.Instance(e)] = true
		}

		// add only if the dest rule is exported with . or * or explicit exportTo containing this namespace
		// The global exportTo doesn't matter here (its either . or * - both of which are applicable here)
		if len(exportToMap) == 0 || exportToMap[visibility.Public] || exportToMap[visibility.Private] ||
			exportToMap[visibility.Instance(configs[i].Namespace)] {
			// Store in an index for the config's namespace
			// a proxy from this namespace will first look here for the destination rule for a given service
			// This pool consists of both public/private destination rules.
			if _, exist := namespaceLocalDestRules[configs[i].Namespace]; !exist {
				namespaceLocalDestRules[configs[i].Namespace] = newProcessedDestRules()
			}
			// Merge this destination rule with any public/private dest rules for same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			ps.mergeDestinationRule(namespaceLocalDestRules[configs[i].Namespace], configs[i], exportToMap)
		}

		isPrivateOnly := false
		// No exportTo in destinationRule. Use the global default
		// We only honor . and *
		if len(rule.ExportTo) == 0 && ps.exportToDefaults.destinationRule[visibility.Private] {
			isPrivateOnly = true
		} else if len(rule.ExportTo) == 1 && exportToMap[visibility.Private] {
			isPrivateOnly = true
		}

		if !isPrivateOnly {
			if _, exist := exportedDestRulesByNamespace[configs[i].Namespace]; !exist {
				exportedDestRulesByNamespace[configs[i].Namespace] = newProcessedDestRules()
			}
			// Merge this destination rule with any other exported dest rule for the same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			ps.mergeDestinationRule(exportedDestRulesByNamespace[configs[i].Namespace], configs[i], exportToMap)
		} else if configs[i].Namespace == ps.Mesh.RootNamespace {
			// Keep track of private root namespace destination rules
			ps.mergeDestinationRule(rootNamespaceLocalDestRules, configs[i], exportToMap)
		}
	}

	// precompute DestinationRules with inherited fields
	if features.EnableDestinationRuleInheritance {
		globalRule := inheritedConfigs[ps.Mesh.RootNamespace]
		for ns := range namespaceLocalDestRules {
			nsRule := inheritedConfigs[ns]
			inheritedRule := ps.inheritDestinationRule(globalRule, nsRule)
			for hostname, cfg := range namespaceLocalDestRules[ns].destRule {
				namespaceLocalDestRules[ns].destRule[hostname] = ps.inheritDestinationRule(inheritedRule, cfg)
			}
			// update namespace rule after it has been merged with mesh rule
			inheritedConfigs[ns] = inheritedRule
		}
		// can't precalculate exportedDestRulesByNamespace since we don't know all the client namespaces in advance
		// inheritance is performed in getExportedDestinationRuleFromNamespace
	}

	// presort it so that we don't sort it for each DestinationRule call.
	// sort.Sort for Hostnames will automatically sort from the most specific to least specific
	for ns := range namespaceLocalDestRules {
		sort.Sort(host.Names(namespaceLocalDestRules[ns].hosts))
	}
	for ns := range exportedDestRulesByNamespace {
		sort.Sort(host.Names(exportedDestRulesByNamespace[ns].hosts))
	}

	ps.destinationRuleIndex.namespaceLocal = namespaceLocalDestRules
	ps.destinationRuleIndex.exportedByNamespace = exportedDestRulesByNamespace
	ps.destinationRuleIndex.rootNamespaceLocal = rootNamespaceLocalDestRules
	ps.destinationRuleIndex.inheritedByNamespace = inheritedConfigs
}

func (ps *PushContext) initAuthorizationPolicies(env *Environment) error {
	var err error
	if ps.AuthzPolicies, err = GetAuthorizationPolicies(env); err != nil {
		authzLog.Errorf("failed to initialize authorization policies: %v", err)
		return err
	}
	return nil
}

// pre computes envoy filters per namespace
func (ps *PushContext) initEnvoyFilters(env *Environment) error {
	envoyFilterConfigs, err := env.List(gvk.EnvoyFilter, NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(envoyFilterConfigs)

	ps.envoyFiltersByNamespace = make(map[string][]*EnvoyFilterWrapper)
	for _, envoyFilterConfig := range envoyFilterConfigs {
		efw := convertToEnvoyFilterWrapper(&envoyFilterConfig)
		if _, exists := ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace]; !exists {
			ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace] = make([]*EnvoyFilterWrapper, 0)
		}
		ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace] = append(ps.envoyFiltersByNamespace[envoyFilterConfig.Namespace], efw)
	}
	return nil
}

// EnvoyFilters return the merged EnvoyFilterWrapper of a proxy
func (ps *PushContext) EnvoyFilters(proxy *Proxy) *EnvoyFilterWrapper {
	// this should never happen
	if proxy == nil {
		return nil
	}
	matchedEnvoyFilters := make([]*EnvoyFilterWrapper, 0)
	// EnvoyFilters supports inheritance (global ones plus namespace local ones).
	// First get all the filter configs from the config root namespace
	// and then add the ones from proxy's own namespace
	if ps.Mesh.RootNamespace != "" {
		// if there is no workload selector, the config applies to all workloads
		// if there is a workload selector, check for matching workload labels
		for _, efw := range ps.envoyFiltersByNamespace[ps.Mesh.RootNamespace] {
			var workloadLabels labels.Collection
			// This should never happen except in tests.
			if proxy.Metadata != nil && len(proxy.Metadata.Labels) > 0 {
				workloadLabels = labels.Collection{proxy.Metadata.Labels}
			}
			if efw.workloadSelector == nil || workloadLabels.IsSupersetOf(efw.workloadSelector) {
				matchedEnvoyFilters = append(matchedEnvoyFilters, efw)
			}
		}
	}

	// To prevent duplicate envoyfilters in case root namespace equals proxy's namespace
	if proxy.ConfigNamespace != ps.Mesh.RootNamespace {
		for _, efw := range ps.envoyFiltersByNamespace[proxy.ConfigNamespace] {
			var workloadLabels labels.Collection
			// This should never happen except in tests.
			if proxy.Metadata != nil && len(proxy.Metadata.Labels) > 0 {
				workloadLabels = labels.Collection{proxy.Metadata.Labels}
			}
			if efw.workloadSelector == nil || workloadLabels.IsSupersetOf(efw.workloadSelector) {
				matchedEnvoyFilters = append(matchedEnvoyFilters, efw)
			}
		}
	}

	var out *EnvoyFilterWrapper
	if len(matchedEnvoyFilters) > 0 {
		out = &EnvoyFilterWrapper{
			// no need populate workloadSelector, as it is not used later.
			Patches: make(map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper),
		}
		// merge EnvoyFilterWrapper
		for _, efw := range matchedEnvoyFilters {
			for applyTo, cps := range efw.Patches {
				if out.Patches[applyTo] == nil {
					out.Patches[applyTo] = []*EnvoyFilterConfigPatchWrapper{}
				}
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

// pre computes gateways per namespace
func (ps *PushContext) initGateways(env *Environment) error {
	gatewayConfigs, err := env.List(gvk.Gateway, NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(gatewayConfigs)

	ps.gatewayIndex.all = gatewayConfigs
	ps.gatewayIndex.namespace = make(map[string][]config.Config)
	for _, gatewayConfig := range gatewayConfigs {
		if _, exists := ps.gatewayIndex.namespace[gatewayConfig.Namespace]; !exists {
			ps.gatewayIndex.namespace[gatewayConfig.Namespace] = make([]config.Config, 0)
		}
		ps.gatewayIndex.namespace[gatewayConfig.Namespace] = append(ps.gatewayIndex.namespace[gatewayConfig.Namespace], gatewayConfig)
	}
	return nil
}

func (ps *PushContext) mergeGateways(proxy *Proxy) *MergedGateway {
	// this should never happen
	if proxy == nil {
		return nil
	}
	out := make([]config.Config, 0)

	var configs []config.Config
	if features.ScopeGatewayToNamespace {
		configs = ps.gatewayIndex.namespace[proxy.ConfigNamespace]
	} else {
		configs = ps.gatewayIndex.all
	}

	// Get the target ports of the service
	targetPorts := make(map[uint32]uint32)
	servicePorts := make(map[uint32]uint32)
	for _, si := range proxy.ServiceInstances {
		targetPorts[si.Endpoint.EndpointPort] = uint32(si.ServicePort.Port)
		servicePorts[uint32(si.ServicePort.Port)] = si.Endpoint.EndpointPort
	}
	for _, cfg := range configs {
		gw := cfg.Spec.(*networking.Gateway)
		selected := false
		if gw.GetSelector() == nil {
			// no selector. Applies to all workloads asking for the gateway
			selected = true
		} else {
			gatewaySelector := labels.Instance(gw.GetSelector())
			var workloadLabels labels.Collection
			// This should never happen except in tests.
			if proxy.Metadata != nil && len(proxy.Metadata.Labels) > 0 {
				workloadLabels = labels.Collection{proxy.Metadata.Labels}
			}
			if workloadLabels.IsSupersetOf(gatewaySelector) {
				selected = true
			}
		}
		if selected {
			// rewritePorts records index of gateway server port that needs to be rewritten.
			rewritePorts := make(map[int]uint32)
			for i, s := range gw.Servers {
				if servicePort, ok := targetPorts[s.Port.Number]; ok && servicePort != s.Port.Number {
					// Check if the gateway server port is also defined as a service port, if so skip rewriting since it is
					// ambiguous on whether the server port points to service port or target port.
					if _, ok := servicePorts[s.Port.Number]; ok {
						continue
					}

					// The gateway server is defined with target port. Convert it to service port before gateway merging.
					// Gateway listeners are based on target port, this prevents duplicated listeners be generated when build
					// listener resources based on merged gateways.
					rewritePorts[i] = servicePort
				}
			}
			if len(rewritePorts) != 0 {
				// Make a deep copy of the gateway configuration and rewrite server port with service port.
				newGWConfig := cfg.DeepCopy()
				newGW := newGWConfig.Spec.(*networking.Gateway)
				for ind, sp := range rewritePorts {
					newGW.Servers[ind].Port.Number = sp
				}
				out = append(out, newGWConfig)
			} else {
				out = append(out, cfg)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return MergeGateways(out...)
}

// pre computes gateways for each network
func (ps *PushContext) initMeshNetworks(meshNetworks *meshconfig.MeshNetworks) {
	ps.networksMu.Lock()
	defer ps.networksMu.Unlock()
	ps.networkGateways = map[string][]*Gateway{}

	// First, use addresses directly specified in meshNetworks
	if meshNetworks != nil {
		for network, networkConf := range meshNetworks.Networks {
			gws := networkConf.Gateways
			for _, gw := range gws {
				if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
					ps.networkGateways[network] = append(ps.networkGateways[network], &Gateway{gw.GetAddress(), gw.Port})
				}
			}

		}
	}

	// Second, load registry specific gateways.
	for network, gateways := range ps.ServiceDiscovery.NetworkGateways() {
		// - the internal map of label gateways - these get deleted if the service is deleted, updated if the ip changes etc.
		// - the computed map from meshNetworks (triggered by reloadNetworkLookup, the ported logic from getGatewayAddresses)
		ps.networkGateways[network] = append(ps.networkGateways[network], gateways...)
	}
}

func (ps *PushContext) NetworkGateways() map[string][]*Gateway {
	ps.networksMu.RLock()
	defer ps.networksMu.RUnlock()
	return ps.networkGateways
}

func (ps *PushContext) NetworkGatewaysByNetwork(network string) []*Gateway {
	ps.networksMu.RLock()
	defer ps.networksMu.RUnlock()
	if ps.networkGateways != nil {
		return ps.networkGateways[network]
	}

	return nil
}

// BestEffortInferServiceMTLSMode infers the mTLS mode for the service + port from all authentication
// policies (both alpha and beta) in the system. The function always returns MTLSUnknown for external service.
// The result is a best effort. It is because the PeerAuthentication is workload-based, this function is unable
// to compute the correct service mTLS mode without knowing service to workload binding. For now, this
// function uses only mesh and namespace level PeerAuthentication and ignore workload & port level policies.
// This function is used to give a hint for auto-mTLS configuration on client side.
func (ps *PushContext) BestEffortInferServiceMTLSMode(tp *networking.TrafficPolicy, service *Service, port *Port) MutualTLSMode {
	if service.MeshExternal {
		// Only need the authentication MTLS mode when service is not external.
		return MTLSUnknown
	}

	// For passthrough traffic (headless service or explicitly defined in DestinationRule), we look at the instances
	// If ALL instances have a sidecar, we enable TLS, otherwise we disable
	// TODO(https://github.com/istio/istio/issues/27376) enable mixed deployments
	// A service with passthrough resolution is always passthrough, regardless of the TrafficPolicy.
	if service.Resolution == Passthrough || tp.GetLoadBalancer().GetSimple() == networking.LoadBalancerSettings_PASSTHROUGH {
		instances := ps.ServiceInstancesByPort(service, port.Port, nil)
		if len(instances) == 0 {
			return MTLSDisable
		}
		for _, i := range instances {
			// Infer mTls disabled if any of the endpoint is with tls disabled
			if i.Endpoint.TLSMode == DisabledTLSModeLabel {
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

// ServiceInstancesByPort returns the cached instances by port if it exists, otherwise queries the discovery and returns.
func (ps *PushContext) ServiceInstancesByPort(svc *Service, port int, labels labels.Collection) []*ServiceInstance {
	// Use cached version of instances by port when labels are empty. If there are labels,
	// we will have to make actual call and filter instances by pod labels.
	if len(labels) == 0 {
		if instances, exists := ps.ServiceIndex.instancesByPort[svc][port]; exists {
			return instances
		}
	}

	// Fallback to discovery call.
	return ps.InstancesByPort(svc, port, labels)
}
