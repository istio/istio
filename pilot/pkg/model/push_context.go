// Copyright 2018 Istio Authors
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
	"strings"
	"sync"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/monitoring"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/visibility"
)

var (
	defaultClusterLocalNamespaces = []string{"kube-system"}
)

// Metrics is an interface for capturing metrics on a per-node basis.
type Metrics interface {
	// AddMetric will add an case to the metric for the given node.
	AddMetric(metric monitoring.Metric, key string, proxy *Proxy, msg string)
}

var _ Metrics = &PushContext{}

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

	// Mutex is used to protect the below store.
	// All data is set when the PushContext object is populated in `InitContext`,
	// data should not be changed by plugins.
	Mutex sync.Mutex `json:"-"`

	// Synthesized from env.Mesh
	defaultServiceExportTo         map[visibility.Instance]bool
	defaultVirtualServiceExportTo  map[visibility.Instance]bool
	defaultDestinationRuleExportTo map[visibility.Instance]bool

	// Service related
	// TODO: move to a sub struct

	// privateServices are reachable within the same namespace.
	privateServicesByNamespace map[string][]*Service
	// publicServices are services reachable within the mesh.
	publicServices []*Service
	// ServiceByHostnameAndNamespace has all services, indexed by hostname then namespace.
	ServiceByHostnameAndNamespace map[host.Name]map[string]*Service `json:"-"`
	// ServiceAccounts contains a map of hostname and port to service accounts.
	ServiceAccounts map[host.Name]map[int][]string `json:"-"`
	// QuotaSpec has all quota specs
	QuotaSpec []Config `json:"-"`
	// QuotaSpecBindings has all quota bindings
	QuotaSpecBinding []Config `json:"-"`

	// VirtualService related
	// this contains all the virtual services with exportTo "." and current namespace. The keys are namespace,gateway.
	privateVirtualServicesByNamespaceAndGateway map[string]map[string][]Config
	// This contains all virtual services whose exportTo is "*", keyed by gateway
	publicVirtualServicesByGateway map[string][]Config

	// destination rules are of two types:
	//  namespaceLocalDestRules: all public/private dest rules pertaining to a service defined in a given namespace
	//  namespaceExportedDestRules: all public dest rules pertaining to a service defined in a namespace
	namespaceLocalDestRules    map[string]*processedDestRules
	namespaceExportedDestRules map[string]*processedDestRules

	// clusterLocalHosts extracted from the MeshConfig
	clusterLocalHosts host.Names

	// sidecars for each namespace
	sidecarsByNamespace map[string][]*SidecarScope
	// envoy filters for each namespace including global config namespace
	envoyFiltersByNamespace map[string][]*EnvoyFilterWrapper
	// gateways for each namespace
	gatewaysByNamespace map[string][]Config
	allGateways         []Config
	////////// END ////////

	// The following data is either a global index or used in the inbound path.
	// Namespace specific views do not apply here.

	// AuthzPolicies stores the existing authorization policies in the cluster. Could be nil if there
	// are no authorization policies in the cluster.
	AuthzPolicies *AuthorizationPolicies `json:"-"`

	// Mesh configuration for the mesh.
	Mesh *meshconfig.MeshConfig `json:"-"`

	// Networks configuration.
	Networks *meshconfig.MeshNetworks `json:"-"`

	// Discovery interface for listing services and instances.
	ServiceDiscovery `json:"-"`

	// Config interface for listing routing rules
	IstioConfigStore `json:"-"`

	// AuthnBetaPolicies contains (beta) Authn policies by namespace.
	AuthnBetaPolicies *AuthenticationPolicies `json:"-"`

	initDone bool

	Version string

	// cache gateways addresses for each network
	// this is mainly used for kubernetes multi-cluster scenario
	networkGateways map[string][]*Gateway
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
	// Map of dest rule host and the merged destination rules for that host
	destRule map[host.Name]*Config
}

// XDSUpdater is used for direct updates of the xDS model and incremental push.
// Pilot uses multiple registries - for example each K8S cluster is a registry instance,
// as well as consul and future EDS or MCP sources. Each registry is responsible for
// tracking a set of endpoints associated with mesh services, and calling the EDSUpdate
// on changes. A registry may group endpoints for a service in smaller subsets - for
// example by deployment, or to deal with very large number of endpoints for a service.
// We want to avoid passing around large objects - like full list of endpoints for a registry,
// or the full list of endpoints for a service across registries, since it limits scalability.
//
// Future optimizations will include grouping the endpoints by labels, gateway or region to
// reduce the time when subsetting or split-horizon is used. This design assumes pilot
// tracks all endpoints in the mesh and they fit in RAM - so limit is few M endpoints.
// It is possible to split the endpoint tracking in future.
type XDSUpdater interface {

	// EDSUpdate is called when the list of endpoints or labels in a ServiceEntry is
	// changed. For each cluster and hostname, the full list of active endpoints (including empty list)
	// must be sent. The shard name is used as a key - current implementation is using the registry
	// name.
	EDSUpdate(shard, hostname string, namespace string, entry []*IstioEndpoint) error

	// SvcUpdate is called when a service definition is updated/deleted.
	SvcUpdate(shard, hostname string, namespace string, event Event)

	// ConfigUpdate is called to notify the XDS server of config updates and request a push.
	// The requests may be collapsed and throttled.
	// This replaces the 'cache invalidation' model.
	ConfigUpdate(req *PushRequest)

	// ProxyUpdate is called to notify the XDS server to send a push to the specified proxy.
	// The requests may be collapsed and throttled.
	ProxyUpdate(clusterID, ip string)
}

// PushRequest defines a request to push to proxies
// It is used to send updates to the config update debouncer and pass to the PushQueue.
type PushRequest struct {
	// Full determines whether a full push is required or not. If set to false, only endpoints will be sent.
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
)

var (
	ServiceEntryKind    = collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind()
	VirtualServiceKind  = collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind()
	DestinationRuleKind = collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind()
)

// Merge two update requests together
func (first *PushRequest) Merge(other *PushRequest) *PushRequest {
	if first == nil {
		return other
	}
	if other == nil {
		return first
	}

	merged := &PushRequest{
		// Keep the first (older) start time
		Start: first.Start,

		// If either is full we need a full push
		Full: first.Full || other.Full,

		// The other push context is presumed to be later and more up to date
		Push: other.Push,

		// Merge the two reasons. Note that we shouldn't deduplicate here, or we would under count
		Reason: append(first.Reason, other.Reason...),
	}

	// Do not merge when any one is empty
	if len(first.ConfigsUpdated) > 0 && len(other.ConfigsUpdated) > 0 {
		merged.ConfigsUpdated = make(map[ConfigKey]struct{}, len(first.ConfigsUpdated)+len(other.ConfigsUpdated))
		for conf := range first.ConfigsUpdated {
			merged.ConfigsUpdated[conf] = struct{}{}
		}
		for conf := range other.ConfigsUpdated {
			merged.ConfigsUpdated[conf] = struct{}{}
		}
	}

	return merged
}

// ProxyPushStatus represents an event captured during config push to proxies.
// It may contain additional message and the affected proxy.
type ProxyPushStatus struct {
	Proxy   string `json:"proxy,omitempty"`
	Message string `json:"message,omitempty"`
}

// IsMixerEnabled returns true if mixer is enabled in the Mesh config.
func (ps *PushContext) IsMixerEnabled() bool {
	return ps != nil && ps.Mesh != nil && (ps.Mesh.MixerCheckServer != "" || ps.Mesh.MixerReportServer != "")
}

// AddMetric will add an case to the metric.
func (ps *PushContext) AddMetric(metric monitoring.Metric, key string, proxy *Proxy, msg string) {
	if ps == nil {
		log.Infof("Metric without context %s %v %s", key, proxy, msg)
		return
	}
	ps.proxyStatusMutex.Lock()
	defer ps.proxyStatusMutex.Unlock()

	metricMap, f := ps.ProxyStatus[metric.Name()]
	if !f {
		metricMap = map[string]ProxyPushStatus{}
		ps.ProxyStatus[metric.Name()] = metricMap
	}
	ev := ProxyPushStatus{Message: msg}
	if proxy != nil {
		ev.Proxy = proxy.ID
	}
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

	// ProxyStatusConflictOutboundListenerTCPOverThrift metric tracks number of
	// TCP listeners that conflicted with existing Thrift listeners on same port
	ProxyStatusConflictOutboundListenerTCPOverThrift = monitoring.NewGauge(
		"pilot_conflict_outbound_listener_tcp_over_current_thrift",
		"Number of conflicting tcp listeners with current thrift listener.",
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
		publicServices:                              []*Service{},
		privateServicesByNamespace:                  map[string][]*Service{},
		publicVirtualServicesByGateway:              map[string][]Config{},
		privateVirtualServicesByNamespaceAndGateway: map[string]map[string][]Config{},
		namespaceLocalDestRules:                     map[string]*processedDestRules{},
		namespaceExportedDestRules:                  map[string]*processedDestRules{},
		sidecarsByNamespace:                         map[string][]*SidecarScope{},
		envoyFiltersByNamespace:                     map[string][]*EnvoyFilterWrapper{},
		gatewaysByNamespace:                         map[string][]Config{},
		allGateways:                                 []Config{},
		ServiceByHostnameAndNamespace:               map[host.Name]map[string]*Service{},
		ProxyStatus:                                 map[string]map[string]ProxyPushStatus{},
		ServiceAccounts:                             map[host.Name]map[int][]string{},
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

	// First add private services
	if proxy == nil {
		for _, privateServices := range ps.privateServicesByNamespace {
			out = append(out, privateServices...)
		}
	} else {
		out = append(out, ps.privateServicesByNamespace[proxy.ConfigNamespace]...)
	}

	// Second add public services
	out = append(out, ps.publicServices...)

	return out
}

func (ps *PushContext) VirtualServicesForGateway(proxy *Proxy, gateway string) []Config {
	res := ps.privateVirtualServicesByNamespaceAndGateway[proxy.ConfigNamespace][gateway]
	res = append(res, ps.publicVirtualServicesByGateway[gateway]...)
	return res
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
		var defaultSidecar *SidecarScope
		for _, wrapper := range sidecars {
			if wrapper.Config != nil {
				sidecar := wrapper.Config.Spec.(*networking.Sidecar)
				// if there is no workload selector, the config applies to all workloads
				// if there is a workload selector, check for matching workload labels
				if sidecar.GetWorkloadSelector() != nil {
					workloadSelector := labels.Instance(sidecar.GetWorkloadSelector().GetLabels())
					if !workloadLabels.IsSupersetOf(workloadSelector) {
						continue
					}
					return wrapper
				}
				defaultSidecar = wrapper
				continue
			}
			// Not sure when this can happen (Config = nil ?)
			if defaultSidecar != nil {
				return defaultSidecar // still return the valid one
			}
			return wrapper
		}
		if defaultSidecar != nil {
			return defaultSidecar // still return the valid one
		}
	}

	return DefaultSidecarScopeForNamespace(ps, proxy.ConfigNamespace)
}

// GetAllSidecarScopes returns a map of namespace and the set of SidecarScope
// object associated with the namespace. This will be used by the CDS code to
// precompute CDS output for each sidecar scope. Since we have a default sidecarscope
// for namespaces that do not explicitly have one, we are guaranteed to
// have the CDS output cached for every namespace/sidecar scope combo.
func (ps *PushContext) GetAllSidecarScopes() map[string][]*SidecarScope {
	return ps.sidecarsByNamespace
}

// DestinationRule returns a destination rule for a service name in a given domain.
func (ps *PushContext) DestinationRule(proxy *Proxy, service *Service) *Config {
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
		if ps.namespaceLocalDestRules[proxy.ConfigNamespace] != nil {
			if hostname, ok := MostSpecificHostMatch(service.Hostname,
				ps.namespaceLocalDestRules[proxy.ConfigNamespace].hosts); ok {
				return ps.namespaceLocalDestRules[proxy.ConfigNamespace].destRule[hostname]
			}
		}
	}

	// 2. select destination rule from service namespace
	svcNs := service.Attributes.Namespace

	// This can happen when finding the subset labels for a proxy in root namespace.
	// Because based on a pure cluster name, we do not know the service and
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
	// check the target service's namespace for public rules
	if svcNs != "" && ps.namespaceExportedDestRules[svcNs] != nil {
		if hostname, ok := MostSpecificHostMatch(service.Hostname,
			ps.namespaceExportedDestRules[svcNs].hosts); ok {
			return ps.namespaceExportedDestRules[svcNs].destRule[hostname]
		}
	}

	// 4. if no public/private rule in calling proxy's namespace matched, and no public rule in the
	// target service's namespace matched, search for any public destination rule in the config root namespace
	// NOTE: This does mean that we are effectively ignoring private dest rules in the config root namespace
	if ps.namespaceExportedDestRules[ps.Mesh.RootNamespace] != nil {
		if hostname, ok := MostSpecificHostMatch(service.Hostname,
			ps.namespaceExportedDestRules[ps.Mesh.RootNamespace].hosts); ok {
			return ps.namespaceExportedDestRules[ps.Mesh.RootNamespace].destRule[hostname]
		}
	}

	return nil
}

// IsClusterLocal indicates whether the endpoints for the service should only be accessible to clients
// within the cluster.
func (ps *PushContext) IsClusterLocal(service *Service) bool {
	_, ok := MostSpecificHostMatch(service.Hostname, ps.clusterLocalHosts)
	return ok
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
	ps.Mutex.Lock()
	defer ps.Mutex.Unlock()
	if ps.initDone {
		return nil
	}

	ps.Mesh = env.Mesh()
	ps.Networks = env.Networks()
	ps.ServiceDiscovery = env
	ps.IstioConfigStore = env
	ps.Version = env.Version()

	// Must be initialized first
	// as initServiceRegistry/VirtualServices/Destrules
	// use the default export map
	ps.initDefaultExportMaps()

	// create new or incremental update
	if pushReq == nil || oldPushContext == nil || !oldPushContext.initDone || len(pushReq.ConfigsUpdated) == 0 {
		if err := ps.createNewContext(env); err != nil {
			return err
		}
	} else {
		if err := ps.updateContext(env, oldPushContext, pushReq); err != nil {
			return err
		}
	}

	// TODO: only do this when meshnetworks or gateway service changed
	ps.initMeshNetworks()

	ps.initClusterLocalHosts(env)

	ps.initDone = true
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

	if err := ps.initQuotaSpecs(env); err != nil {
		return err
	}

	if err := ps.initQuotaSpecBindings(env); err != nil {
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
		authnChanged, authzChanged, envoyFiltersChanged, sidecarsChanged, quotasChanged bool

	for conf := range pushReq.ConfigsUpdated {
		switch conf.Kind {
		case ServiceEntryKind:
			servicesChanged = true
		case DestinationRuleKind:
			destinationRulesChanged = true
		case VirtualServiceKind:
			virtualServicesChanged = true
		case collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind():
			gatewayChanged = true
		case collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind():
			sidecarsChanged = true
		case collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind():
			envoyFiltersChanged = true
		case collections.IstioRbacV1Alpha1Servicerolebindings.Resource().GroupVersionKind(),
			collections.IstioRbacV1Alpha1Serviceroles.Resource().GroupVersionKind(),
			collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().GroupVersionKind(),
			collections.IstioRbacV1Alpha1Rbacconfigs.Resource().GroupVersionKind(),
			collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind():
			authzChanged = true
		case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind(),
			collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
			authnChanged = true
		case collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind(),
			collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind():
			quotasChanged = true
		case collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind(),
			collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind(),
			collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind(),
			collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind(),
			collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind():
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
		ps.privateServicesByNamespace = oldPushContext.privateServicesByNamespace
		ps.publicServices = oldPushContext.publicServices
		ps.ServiceByHostnameAndNamespace = oldPushContext.ServiceByHostnameAndNamespace
		ps.ServiceAccounts = oldPushContext.ServiceAccounts
	}

	if virtualServicesChanged {
		if err := ps.initVirtualServices(env); err != nil {
			return err
		}
	} else {
		ps.privateVirtualServicesByNamespaceAndGateway = oldPushContext.privateVirtualServicesByNamespaceAndGateway
		ps.publicVirtualServicesByGateway = oldPushContext.publicVirtualServicesByGateway
	}

	if destinationRulesChanged {
		if err := ps.initDestinationRules(env); err != nil {
			return err
		}
	} else {
		ps.namespaceLocalDestRules = oldPushContext.namespaceLocalDestRules
		ps.namespaceExportedDestRules = oldPushContext.namespaceExportedDestRules
	}

	if authnChanged {
		if err := ps.initAuthnPolicies(env); err != nil {
			return err
		}
	} else {
		ps.AuthnBetaPolicies = oldPushContext.AuthnBetaPolicies
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
		ps.gatewaysByNamespace = oldPushContext.gatewaysByNamespace
		ps.allGateways = oldPushContext.allGateways
	}

	if quotasChanged {
		if err := ps.initQuotaSpecs(env); err != nil {
			return err
		}
		if err := ps.initQuotaSpecBindings(env); err != nil {
			return err
		}
	} else {
		ps.QuotaSpec = oldPushContext.QuotaSpec
		ps.QuotaSpecBinding = oldPushContext.QuotaSpecBinding
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
		ns := s.Attributes.Namespace
		if len(s.Attributes.ExportTo) == 0 {
			if ps.defaultServiceExportTo[visibility.Private] {
				ps.privateServicesByNamespace[ns] = append(ps.privateServicesByNamespace[ns], s)
			} else if ps.defaultServiceExportTo[visibility.Public] {
				ps.publicServices = append(ps.publicServices, s)
			}
		} else {
			if s.Attributes.ExportTo[visibility.Private] {
				ps.privateServicesByNamespace[ns] = append(ps.privateServicesByNamespace[ns], s)
			} else {
				ps.publicServices = append(ps.publicServices, s)
			}
		}
		if _, f := ps.ServiceByHostnameAndNamespace[s.Hostname]; !f {
			ps.ServiceByHostnameAndNamespace[s.Hostname] = map[string]*Service{}
		}
		ps.ServiceByHostnameAndNamespace[s.Hostname][s.Attributes.Namespace] = s
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
	var initBetaPolicyErro error
	if ps.AuthnBetaPolicies, initBetaPolicyErro = initAuthenticationPolicies(env); initBetaPolicyErro != nil {
		return initBetaPolicyErro
	}

	return nil
}

// Caches list of virtual services
func (ps *PushContext) initVirtualServices(env *Environment) error {
	ps.privateVirtualServicesByNamespaceAndGateway = map[string]map[string][]Config{}
	ps.publicVirtualServicesByGateway = map[string][]Config{}

	virtualServices, err := env.List(collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return err
	}

	// values returned from ConfigStore.List are immutable.
	// Therefore, we make a copy
	vservices := make([]Config, len(virtualServices))

	for i := range vservices {
		vservices[i] = virtualServices[i].DeepCopy()
	}

	totalVirtualServices.Record(float64(len(virtualServices)))

	// TODO(rshriram): parse each virtual service and maintain a map of the
	// virtualservice name, the list of registry hosts in the VS and non
	// registry DNS names in the VS.  This should cut down processing in
	// the RDS code. See separateVSHostsAndServices in route/route.go
	sortConfigByCreationTime(vservices)

	vservices = mergeVirtualServicesIfNeeded(vservices)

	// convert all shortnames in virtual services into FQDNs
	for _, r := range vservices {
		rule := r.Spec.(*networking.VirtualService)
		// resolve top level hosts
		for i, h := range rule.Hosts {
			rule.Hosts[i] = string(ResolveShortnameToFQDN(h, r.ConfigMeta))
		}
		// resolve gateways to bind to
		for i, g := range rule.Gateways {
			if g != constants.IstioMeshGateway {
				rule.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
			}
		}
		// resolve host in http route.destination, route.mirror
		for _, d := range rule.Http {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != constants.IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range d.Route {
				if w.Destination != nil {
					w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
				}
			}
			if d.Mirror != nil {
				d.Mirror.Host = string(ResolveShortnameToFQDN(d.Mirror.Host, r.ConfigMeta))
			}
		}
		// resolve host in tcp route.destination
		for _, d := range rule.Tcp {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != constants.IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range d.Route {
				if w.Destination != nil {
					w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
				}
			}
		}
		//resolve host in tls route.destination
		for _, tls := range rule.Tls {
			for _, m := range tls.Match {
				for i, g := range m.Gateways {
					if g != constants.IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range tls.Route {
				if w.Destination != nil {
					w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
				}
			}
		}
	}

	for _, virtualService := range vservices {
		ns := virtualService.Namespace
		rule := virtualService.Spec.(*networking.VirtualService)
		gwNames := getGatewayNames(rule, virtualService.ConfigMeta)
		if len(rule.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			// TODO: We currently only honor ., * and ~
			if ps.defaultVirtualServiceExportTo[visibility.Private] {
				if _, f := ps.privateVirtualServicesByNamespaceAndGateway[ns]; !f {
					ps.privateVirtualServicesByNamespaceAndGateway[ns] = map[string][]Config{}
				}
				// add to local namespace only
				for _, gw := range gwNames {
					ps.privateVirtualServicesByNamespaceAndGateway[ns][gw] = append(ps.privateVirtualServicesByNamespaceAndGateway[ns][gw], virtualService)
				}
			} else if ps.defaultVirtualServiceExportTo[visibility.Public] {
				for _, gw := range gwNames {
					ps.publicVirtualServicesByGateway[gw] = append(ps.publicVirtualServicesByGateway[gw], virtualService)
				}
			}
		} else {
			// TODO: we currently only process the first element in the array
			// and currently only consider . or * which maps to public/private
			if visibility.Instance(rule.ExportTo[0]) == visibility.Private {
				if _, f := ps.privateVirtualServicesByNamespaceAndGateway[ns]; !f {
					ps.privateVirtualServicesByNamespaceAndGateway[ns] = map[string][]Config{}
				}
				// add to local namespace only
				for _, gw := range gwNames {
					ps.privateVirtualServicesByNamespaceAndGateway[ns][gw] = append(ps.privateVirtualServicesByNamespaceAndGateway[ns][gw], virtualService)
				}
			} else {
				// ~ is not valid in the exportTo fields in virtualServices, services, destination rules
				// and we currently only allow . or *. So treat this as public export
				for _, gw := range gwNames {
					ps.publicVirtualServicesByGateway[gw] = append(ps.publicVirtualServicesByGateway[gw], virtualService)
				}
			}
		}
	}

	return nil
}

var meshGateways = []string{constants.IstioMeshGateway}

func getGatewayNames(vs *networking.VirtualService, meta ConfigMeta) []string {
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
	ps.defaultDestinationRuleExportTo = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultDestinationRuleExportTo != nil {
		for _, e := range ps.Mesh.DefaultDestinationRuleExportTo {
			ps.defaultDestinationRuleExportTo[visibility.Instance(e)] = true
		}
	} else {
		// default to *
		ps.defaultDestinationRuleExportTo[visibility.Public] = true
	}

	ps.defaultServiceExportTo = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultServiceExportTo {
			ps.defaultServiceExportTo[visibility.Instance(e)] = true
		}
	} else {
		ps.defaultServiceExportTo[visibility.Public] = true
	}

	ps.defaultVirtualServiceExportTo = make(map[visibility.Instance]bool)
	if ps.Mesh.DefaultVirtualServiceExportTo != nil {
		for _, e := range ps.Mesh.DefaultVirtualServiceExportTo {
			ps.defaultVirtualServiceExportTo[visibility.Instance(e)] = true
		}
	} else {
		ps.defaultVirtualServiceExportTo[visibility.Public] = true
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
	sidecarConfigs, err := env.List(collections.IstioNetworkingV1Alpha3Sidecars.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(sidecarConfigs)

	sidecarConfigWithSelector := make([]Config, 0)
	sidecarConfigWithoutSelector := make([]Config, 0)
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
	sidecarConfigs = make([]Config, 0, sidecarNum)
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
	var rootNSConfig *Config
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
	for _, nsMap := range ps.ServiceByHostnameAndNamespace {
		for ns := range nsMap {
			if _, exist := sidecarsWithoutSelectorByNamespace[ns]; !exist {
				ps.sidecarsByNamespace[ns] = append(ps.sidecarsByNamespace[ns], ConvertToSidecarScope(ps, rootNSConfig, ns))
			}
		}
	}

	return nil
}

// Split out of DestinationRule expensive conversions - once per push.
func (ps *PushContext) initDestinationRules(env *Environment) error {
	configs, err := env.List(collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return err
	}

	// values returned from ConfigStore.List are immutable.
	// Therefore, we make a copy
	destRules := make([]Config, len(configs))
	for i := range destRules {
		destRules[i] = configs[i].DeepCopy()
	}

	ps.SetDestinationRules(destRules)
	return nil
}

// SetDestinationRules is updates internal structures using a set of configs.
// Split out of DestinationRule expensive conversions, computed once per push.
// This also allows tests to inject a config without having the mock.
// This will not work properly for Sidecars, which will precompute their destination rules on init
func (ps *PushContext) SetDestinationRules(configs []Config) {
	// Sort by time first. So if two destination rule have top level traffic policies
	// we take the first one.
	sortConfigByCreationTime(configs)
	namespaceLocalDestRules := make(map[string]*processedDestRules)
	namespaceExportedDestRules := make(map[string]*processedDestRules)

	for i := range configs {
		rule := configs[i].Spec.(*networking.DestinationRule)
		rule.Host = string(ResolveShortnameToFQDN(rule.Host, configs[i].ConfigMeta))
		// Store in an index for the config's namespace
		// a proxy from this namespace will first look here for the destination rule for a given service
		// This pool consists of both public/private destination rules.
		// TODO: when exportTo is fully supported, only add the rule here if exportTo is '.'
		// The global exportTo doesn't matter here (its either . or * - both of which are applicable here)
		if _, exist := namespaceLocalDestRules[configs[i].Namespace]; !exist {
			namespaceLocalDestRules[configs[i].Namespace] = &processedDestRules{
				hosts:    make([]host.Name, 0),
				destRule: map[host.Name]*Config{},
			}
		}
		// Merge this destination rule with any public/private dest rules for same host in the same namespace
		// If there are no duplicates, the dest rule will be added to the list
		ps.mergeDestinationRule(namespaceLocalDestRules[configs[i].Namespace], configs[i])
		isPubliclyExported := false
		if len(rule.ExportTo) == 0 {
			// No exportTo in destinationRule. Use the global default
			// TODO: We currently only honor ., * and ~
			if ps.defaultDestinationRuleExportTo[visibility.Public] {
				isPubliclyExported = true
			}
		} else {
			// TODO: we currently only process the first element in the array
			// and currently only consider . or * which maps to public/private
			if visibility.Instance(rule.ExportTo[0]) != visibility.Private {
				// ~ is not valid in the exportTo fields in virtualServices, services, destination rules
				// and we currently only allow . or *. So treat this as public export
				isPubliclyExported = true
			}
		}

		if isPubliclyExported {
			if _, exist := namespaceExportedDestRules[configs[i].Namespace]; !exist {
				namespaceExportedDestRules[configs[i].Namespace] = &processedDestRules{
					hosts:    make([]host.Name, 0),
					destRule: map[host.Name]*Config{},
				}
			}
			// Merge this destination rule with any public dest rule for the same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			ps.mergeDestinationRule(namespaceExportedDestRules[configs[i].Namespace], configs[i])
		}
	}

	// presort it so that we don't sort it for each DestinationRule call.
	// sort.Sort for Hostnames will automatically sort from the most specific to least specific
	for ns := range namespaceLocalDestRules {
		sort.Sort(host.Names(namespaceLocalDestRules[ns].hosts))
	}
	for ns := range namespaceExportedDestRules {
		sort.Sort(host.Names(namespaceExportedDestRules[ns].hosts))
	}

	ps.namespaceLocalDestRules = namespaceLocalDestRules
	ps.namespaceExportedDestRules = namespaceExportedDestRules
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
	envoyFilterConfigs, err := env.List(collections.IstioNetworkingV1Alpha3Envoyfilters.Resource().GroupVersionKind(), NamespaceAll)
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

	return out
}

// pre computes gateways per namespace
func (ps *PushContext) initGateways(env *Environment) error {
	gatewayConfigs, err := env.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(gatewayConfigs)

	ps.allGateways = gatewayConfigs
	ps.gatewaysByNamespace = make(map[string][]Config)
	for _, gatewayConfig := range gatewayConfigs {
		if _, exists := ps.gatewaysByNamespace[gatewayConfig.Namespace]; !exists {
			ps.gatewaysByNamespace[gatewayConfig.Namespace] = make([]Config, 0)
		}
		ps.gatewaysByNamespace[gatewayConfig.Namespace] = append(ps.gatewaysByNamespace[gatewayConfig.Namespace], gatewayConfig)
	}
	return nil
}

func (ps *PushContext) mergeGateways(proxy *Proxy) *MergedGateway {
	// this should never happen
	if proxy == nil {
		return nil
	}
	out := make([]Config, 0)

	var configs []Config
	if features.ScopeGatewayToNamespace {
		configs = ps.gatewaysByNamespace[proxy.ConfigNamespace]
	} else {
		configs = ps.allGateways
	}

	for _, cfg := range configs {
		gw := cfg.Spec.(*networking.Gateway)
		if gw.GetSelector() == nil {
			// no selector. Applies to all workloads asking for the gateway
			out = append(out, cfg)
		} else {
			gatewaySelector := labels.Instance(gw.GetSelector())
			var workloadLabels labels.Collection
			// This should never happen except in tests.
			if proxy.Metadata != nil && len(proxy.Metadata.Labels) > 0 {
				workloadLabels = labels.Collection{proxy.Metadata.Labels}
			}
			if workloadLabels.IsSupersetOf(gatewaySelector) {
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
func (ps *PushContext) initMeshNetworks() {
	if ps.Networks == nil || len(ps.Networks.Networks) == 0 {
		return
	}

	ps.networkGateways = map[string][]*Gateway{}
	for network, networkConf := range ps.Networks.Networks {
		gws := networkConf.Gateways
		if len(gws) == 0 {
			// all endpoints in this network are reachable directly from others. nothing to do.
			continue
		}

		registryNames := getNetworkRegistries(networkConf)
		gateways := []*Gateway{}

		for _, gw := range gws {
			gateways = append(gateways, getGatewayAddresses(gw, registryNames, ps.ServiceDiscovery)...)
		}

		log.Debugf("Endpoints from registries %v on network %v reachable through %d gateways",
			registryNames, network, len(gateways))

		ps.networkGateways[network] = gateways
	}
}

func (ps *PushContext) initClusterLocalHosts(e *Environment) {
	// Create the default list of cluster-local hosts.
	domainSuffix := e.GetDomainSuffix()
	defaultClusterLocalHosts := make([]host.Name, 0, len(defaultClusterLocalNamespaces))
	for _, n := range defaultClusterLocalNamespaces {
		defaultClusterLocalHosts = append(defaultClusterLocalHosts, host.Name("*."+n+".svc."+domainSuffix))
	}

	if discoveryHost, err := e.GetDiscoveryHost(); err != nil {
		log.Errorf("failed to make discoveryAddress cluster-local: %v", err)
	} else {
		if !strings.HasSuffix(string(discoveryHost), domainSuffix) {
			discoveryHost += host.Name("." + domainSuffix)
		}
		defaultClusterLocalHosts = append(defaultClusterLocalHosts, discoveryHost)
	}

	// Collect the cluster-local hosts.
	clusterLocalHosts := make([]host.Name, 0)
	for _, serviceSettings := range ps.Mesh.ServiceSettings {
		if serviceSettings.Settings.ClusterLocal {
			for _, h := range serviceSettings.Hosts {
				clusterLocalHosts = append(clusterLocalHosts, host.Name(h))
			}
		} else {
			// Remove defaults if specified to be non-cluster-local.
			for _, h := range serviceSettings.Hosts {
				for i, defaultClusterLocalHost := range defaultClusterLocalHosts {
					if len(defaultClusterLocalHost) > 0 && strings.HasSuffix(h, string(defaultClusterLocalHost[1:])) {
						// This default was explicitly overridden, so remove it.
						defaultClusterLocalHosts[i] = ""
					}
				}
			}
		}
	}

	// Add any remaining defaults to the end of the list.
	for _, defaultClusterLocalHost := range defaultClusterLocalHosts {
		if len(defaultClusterLocalHost) > 0 {
			clusterLocalHosts = append(clusterLocalHosts, defaultClusterLocalHost)
		}
	}

	sort.Sort(host.Names(clusterLocalHosts))
	ps.clusterLocalHosts = clusterLocalHosts
}

func (ps *PushContext) initQuotaSpecs(env *Environment) error {
	var err error
	ps.QuotaSpec, err = env.List(collections.IstioMixerV1ConfigClientQuotaspecs.Resource().GroupVersionKind(), NamespaceAll)
	return err
}

func (ps *PushContext) initQuotaSpecBindings(env *Environment) error {
	var err error
	ps.QuotaSpecBinding, err = env.List(collections.IstioMixerV1ConfigClientQuotaspecbindings.Resource().GroupVersionKind(), NamespaceAll)
	return err
}

func getNetworkRegistries(network *meshconfig.Network) []string {
	var registryNames []string
	for _, eps := range network.Endpoints {
		if eps != nil && len(eps.GetFromRegistry()) > 0 {
			registryNames = append(registryNames, eps.GetFromRegistry())
		}
	}
	return registryNames
}

func getGatewayAddresses(gw *meshconfig.Network_IstioNetworkGateway, registryNames []string, discovery ServiceDiscovery) []*Gateway {
	// First, if a gateway address is provided in the configuration use it. If the gateway address
	// in the config was a hostname it got already resolved and replaced with an IP address
	// when loading the config
	if gwIP := net.ParseIP(gw.GetAddress()); gwIP != nil {
		return []*Gateway{{gw.GetAddress(), gw.Port}}
	}

	// Second, try to find the gateway addresses by the provided service name
	if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
		svc, _ := discovery.GetService(host.Name(gwSvcName))
		if svc == nil {
			return nil
		}
		// No need lock here as the service returned is a new one
		if svc.Attributes.ClusterExternalAddresses != nil {
			var gateways []*Gateway
			for _, clusterName := range registryNames {
				remotePort := gw.Port
				// check if we have node port mappings
				if svc.Attributes.ClusterExternalPorts != nil {
					if nodePortMap, exists := svc.Attributes.ClusterExternalPorts[clusterName]; exists {
						// what we now have is a service port. If there is a mapping for cluster external ports,
						// look it up and get the node port for the remote port
						if nodePort, exists := nodePortMap[remotePort]; exists {
							remotePort = nodePort
						}
					}
				}
				ips := svc.Attributes.ClusterExternalAddresses[clusterName]
				for _, ip := range ips {
					gateways = append(gateways, &Gateway{ip, remotePort})
				}
			}
			return gateways
		}
	}

	return nil
}

func (ps *PushContext) NetworkGateways() map[string][]*Gateway {
	return ps.networkGateways
}

func (ps *PushContext) NetworkGatewaysByNetwork(network string) []*Gateway {
	if ps.networkGateways != nil {
		return ps.networkGateways[network]
	}

	return nil
}

func (ps *PushContext) QuotaSpecByDestination(hostname host.Name) []Config {
	return filterQuotaSpecsByDestination(hostname, ps.QuotaSpecBinding, ps.QuotaSpec)
}

// BestEffortInferServiceMTLSMode infers the mTLS mode for the service + port from all authentication
// policies (both alpha and beta) in the system. The function always returns MTLSUnknown for external service.
// The resulst is a best effort. It is because the PeerAuthentication is workload-based, this function is unable
// to compute the correct service mTLS mode without knowing service to workload binding. For now, this
// function uses only mesh and namespace level PeerAuthentication and ignore workload & port level policies.
// This function is used to give a hint for auto-mTLS configuration on client side.
func (ps *PushContext) BestEffortInferServiceMTLSMode(service *Service, port *Port) MutualTLSMode {
	if service.MeshExternal {
		// Only need the authentication MTLS mode when service is not external.
		return MTLSUnknown
	}

	// First , check mTLS settings from beta policy (i.e PeerAuthentication) at namespace / mesh level.
	// If the mode is not unknown, use it.
	if serviceMTLSMode := ps.AuthnBetaPolicies.GetNamespaceMutualTLSMode(service.Attributes.Namespace); serviceMTLSMode != MTLSUnknown {
		return serviceMTLSMode
	}

	// When all are failed, default to permissive.
	return MTLSPermissive
}
