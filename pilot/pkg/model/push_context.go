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
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	networking "istio.io/api/networking/v1alpha3"
)

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

	// Start represents the time of last config change that reset the
	// push status.
	Start time.Time
	End   time.Time

	// Mutex is used to protect the below store.
	// All data is set when the PushContext object is populated in `InitContext`,
	// data should not be changed by plugins.
	Mutex sync.Mutex `json:"-"`

	// Synthesized from env.Mesh
	defaultServiceExportTo         map[Visibility]bool
	defaultVirtualServiceExportTo  map[Visibility]bool
	defaultDestinationRuleExportTo map[Visibility]bool

	// privateServices are reachable within the same namespace.
	privateServicesByNamespace map[string][]*Service
	// publicServices are services reachable within the mesh.
	publicServices []*Service

	privateVirtualServicesByNamespace map[string][]Config
	publicVirtualServices             []Config

	// destination rules are of three types:
	//  namespaceLocalDestRules: all public/private dest rules pertaining to a service defined in a given namespace
	//  namespaceExportedDestRules: all public dest rules pertaining to a service defined in a namespace
	//  allExportedDestRules: all (public) dest rules across all namespaces
	// We need the allExportedDestRules in addition to namespaceExportedDestRules because we select
	// the dest rule based on the most specific host match, and not just any destination rule
	namespaceLocalDestRules    map[string]*processedDestRules
	namespaceExportedDestRules map[string]*processedDestRules
	allExportedDestRules       *processedDestRules

	// sidecars for each namespace
	sidecarsByNamespace map[string][]*SidecarScope
	////////// END ////////

	// The following data is either a global index or used in the inbound path.
	// Namespace specific views do not apply here.

	// ServiceByHostname has all services, indexed by hostname.
	ServiceByHostname map[Hostname]*Service `json:"-"`

	// AuthzPolicies stores the existing authorization policies in the cluster. Could be nil if there
	// are no authorization policies in the cluster.
	AuthzPolicies *AuthorizationPolicies `json:"-"`

	// Env has a pointer to the shared environment used to create the snapshot.
	Env *Environment `json:"-"`

	// ServicePort2Name is used to keep track of service name and port mapping.
	// This is needed because ADS names use port numbers, while endpoints use
	// port names. The key is the service name. If a service or port are not found,
	// the endpoint needs to be re-evaluated later (eventual consistency)
	ServicePort2Name map[string]PortList `json:"-"`

	// ServiceAccounts contains a map of hostname and port to service accounts.
	ServiceAccounts map[Hostname]map[int][]string `json:"-"`

	initDone bool
}

type processedDestRules struct {
	// List of dest rule hosts. We match with the most specific host first
	hosts []Hostname
	// Map of dest rule host and the merged destination rules for that host
	destRule map[Hostname]*combinedDestinationRule
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
	EDSUpdate(shard, hostname string, entry []*IstioEndpoint) error

	// SvcUpdate is called when a service port mapping definition is updated.
	// This interface is WIP - labels, annotations and other changes to service may be
	// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
	// LDS/RDS.
	SvcUpdate(shard, hostname string, ports map[string]uint32, rports map[uint32]string)

	// WorkloadUpdate is called by a registry when the labels or annotations on a workload have changed.
	// The 'id' is the IP address of the pod for k8s if the pod is in the main/default network.
	// In future it will include the 'network id' for pods in a different network, behind a zvpn gate.
	// The IP is used because K8S Endpoints object associated with a Service only include the IP.
	// We use Endpoints to track the membership to a service and readiness.
	WorkloadUpdate(id string, labels map[string]string, annotations map[string]string)

	// ConfigUpdate is called to notify the XDS server of config updates and request a push.
	// The requests may be collapsed and throttled.
	// This replaces the 'cache invalidation' model.
	ConfigUpdate(full bool)
}

// ProxyPushStatus represents an event captured during config push to proxies.
// It may contain additional message and the affected proxy.
type ProxyPushStatus struct {
	Proxy   string `json:"proxy,omitempty"`
	Message string `json:"message,omitempty"`
}

// PushMetric wraps a prometheus metric.
type PushMetric struct {
	Name  string
	gauge prometheus.Gauge
}

type combinedDestinationRule struct {
	subsets map[string]struct{} // list of subsets seen so far
	// We are not doing ports
	config *Config
}

func newPushMetric(name, help string) *PushMetric {
	pm := &PushMetric{
		gauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		}),
		Name: name,
	}
	prometheus.MustRegister(pm.gauge)
	metrics = append(metrics, pm)
	return pm
}

// Add will add an case to the metric.
func (ps *PushContext) Add(metric *PushMetric, key string, proxy *Proxy, msg string) {
	if ps == nil {
		log.Infof("Metric without context %s %v %s", key, proxy, msg)
		return
	}
	ps.proxyStatusMutex.Lock()
	defer ps.proxyStatusMutex.Unlock()

	metricMap, f := ps.ProxyStatus[metric.Name]
	if !f {
		metricMap = map[string]ProxyPushStatus{}
		ps.ProxyStatus[metric.Name] = metricMap
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
	EndpointNoPod = newPushMetric(
		"endpoint_no_pod",
		"Endpoints without an associated pod.",
	)

	// ProxyStatusNoService represents proxies not selected by any service
	// This can be normal - for workloads that act only as client, or are not covered by a Service.
	// It can also be an error, for example in cases the Endpoint list of a service was not updated by the time
	// the sidecar calls.
	// Updated by GetProxyServiceInstances
	ProxyStatusNoService = newPushMetric(
		"pilot_no_ip",
		"Pods not found in the endpoint table, possibly invalid.",
	)

	// ProxyStatusEndpointNotReady represents proxies found not be ready.
	// Updated by GetProxyServiceInstances. Normal condition when starting
	// an app with readiness, error if it doesn't change to 0.
	ProxyStatusEndpointNotReady = newPushMetric(
		"pilot_endpoint_not_ready",
		"Endpoint found in unready state.",
	)

	// ProxyStatusConflictOutboundListenerTCPOverHTTP metric tracks number of
	// wildcard TCP listeners that conflicted with existing wildcard HTTP listener on same port
	ProxyStatusConflictOutboundListenerTCPOverHTTP = newPushMetric(
		"pilot_conflict_outbound_listener_tcp_over_current_http",
		"Number of conflicting wildcard tcp listeners with current wildcard http listener.",
	)

	// ProxyStatusConflictOutboundListenerTCPOverTCP metric tracks number of
	// TCP listeners that conflicted with existing TCP listeners on same port
	ProxyStatusConflictOutboundListenerTCPOverTCP = newPushMetric(
		"pilot_conflict_outbound_listener_tcp_over_current_tcp",
		"Number of conflicting tcp listeners with current tcp listener.",
	)

	// ProxyStatusConflictOutboundListenerHTTPOverTCP metric tracks number of
	// wildcard HTTP listeners that conflicted with existing wildcard TCP listener on same port
	ProxyStatusConflictOutboundListenerHTTPOverTCP = newPushMetric(
		"pilot_conflict_outbound_listener_http_over_current_tcp",
		"Number of conflicting wildcard http listeners with current wildcard tcp listener.",
	)

	// ProxyStatusConflictInboundListener tracks cases of multiple inbound
	// listeners - 2 services selecting the same port of the pod.
	ProxyStatusConflictInboundListener = newPushMetric(
		"pilot_conflict_inbound_listener",
		"Number of conflicting inbound listeners.",
	)

	// DuplicatedClusters tracks duplicate clusters seen while computing CDS
	DuplicatedClusters = newPushMetric(
		"pilot_duplicate_envoy_clusters",
		"Duplicate envoy clusters caused by service entries with same hostname",
	)

	// ProxyStatusClusterNoInstances tracks clusters (services) without workloads.
	ProxyStatusClusterNoInstances = newPushMetric(
		"pilot_eds_no_instances",
		"Number of clusters without instances.",
	)

	// DuplicatedDomains tracks rejected VirtualServices due to duplicated hostname.
	DuplicatedDomains = newPushMetric(
		"pilot_vservice_dup_domain",
		"Virtual services with dup domains.",
	)

	// DuplicatedSubsets tracks duplicate subsets that we rejected while merging multiple destination rules for same host
	DuplicatedSubsets = newPushMetric(
		"pilot_destrule_subsets",
		"Duplicate subsets across destination rules for same host",
	)

	// LastPushStatus preserves the metrics and data collected during lasts global push.
	// It can be used by debugging tools to inspect the push event. It will be reset after each push with the
	// new version.
	LastPushStatus *PushContext
	// LastPushMutex will protect the LastPushStatus
	LastPushMutex sync.Mutex

	// All metrics we registered.
	metrics []*PushMetric
)

// NewPushContext creates a new PushContext structure to track push status.
func NewPushContext() *PushContext {
	// TODO: detect push in progress, don't update status if set
	return &PushContext{
		publicServices:                    []*Service{},
		privateServicesByNamespace:        map[string][]*Service{},
		publicVirtualServices:             []Config{},
		privateVirtualServicesByNamespace: map[string][]Config{},
		namespaceLocalDestRules:           map[string]*processedDestRules{},
		namespaceExportedDestRules:        map[string]*processedDestRules{},
		allExportedDestRules: &processedDestRules{
			hosts:    make([]Hostname, 0),
			destRule: map[Hostname]*combinedDestinationRule{},
		},
		sidecarsByNamespace: map[string][]*SidecarScope{},

		ServiceByHostname: map[Hostname]*Service{},
		ProxyStatus:       map[string]map[string]ProxyPushStatus{},
		ServicePort2Name:  map[string]PortList{},
		ServiceAccounts:   map[Hostname]map[int][]string{},
		Start:             time.Now(),
	}
}

// JSON implements json.Marshaller, with a lock.
func (ps *PushContext) JSON() ([]byte, error) {
	if ps == nil {
		return []byte{'{', '}'}, nil
	}
	ps.proxyStatusMutex.RLock()
	defer ps.proxyStatusMutex.RUnlock()
	return json.MarshalIndent(ps, "", "    ")
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
		mmap, f := ps.ProxyStatus[pm.Name]
		if f {
			pm.gauge.Set(float64(len(mmap)))
		} else {
			pm.gauge.Set(0)
		}
	}
}

// Services returns the list of services that are visible to a Proxy in a given config namespace
func (ps *PushContext) Services(proxy *Proxy) []*Service {
	// If proxy has a sidecar scope that is user supplied, then get the services from the sidecar scope
	// sidecarScope.config is nil if there is no sidecar scope for the namespace
	// TODO: This is a temporary gate until the sidecar implementation is stable. Once its stable, remove the
	// config != nil check
	if proxy != nil && proxy.SidecarScope != nil && proxy.SidecarScope.Config != nil && proxy.Type == SidecarProxy {
		return proxy.SidecarScope.Services()
	}

	out := []*Service{}

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

// VirtualServices lists all virtual services bound to the specified gateways
// This replaces store.VirtualServices. Used only by the gateways
// Sidecars use the egressListener.VirtualServices().
func (ps *PushContext) VirtualServices(proxy *Proxy, gateways map[string]bool) []Config {
	configs := make([]Config, 0)
	out := make([]Config, 0)

	// filter out virtual services not reachable
	// First private virtual service
	if proxy == nil {
		for _, virtualSvcs := range ps.privateVirtualServicesByNamespace {
			configs = append(configs, virtualSvcs...)
		}
	} else {
		configs = append(configs, ps.privateVirtualServicesByNamespace[proxy.ConfigNamespace]...)
	}
	// Second public virtual service
	configs = append(configs, ps.publicVirtualServices...)

	for _, config := range configs {
		rule := config.Spec.(*networking.VirtualService)
		if len(rule.Gateways) == 0 {
			// This rule applies only to IstioMeshGateway
			if gateways[IstioMeshGateway] {
				out = append(out, config)
			}
		} else {
			for _, g := range rule.Gateways {
				// note: Gateway names do _not_ use wildcard matching, so we do not use Hostname.Matches here
				if gateways[resolveGatewayName(g, config.ConfigMeta)] {
					out = append(out, config)
					break
				} else if g == IstioMeshGateway && gateways[g] {
					// "mesh" gateway cannot be expanded into FQDN
					out = append(out, config)
					break
				}
			}
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
func (ps *PushContext) getSidecarScope(proxy *Proxy, workloadLabels LabelsCollection) *SidecarScope {

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
					workloadSelector := Labels(sidecar.GetWorkloadSelector().GetLabels())
					if !workloadLabels.IsSupersetOf(workloadSelector) {
						continue
					}
					return wrapper
				}
				defaultSidecar = wrapper
				continue
			}
			// Not sure when this can heppn (Config = nil ?)
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
// for namespaces that dont explicitly have one, we are guaranteed to
// have the CDS output cached for every namespace/sidecar scope combo.
func (ps *PushContext) GetAllSidecarScopes() map[string][]*SidecarScope {
	return ps.sidecarsByNamespace
}

// DestinationRule returns a destination rule for a service name in a given domain.
func (ps *PushContext) DestinationRule(proxy *Proxy, service *Service) *Config {
	// If proxy has a sidecar scope that is user supplied, then get the destination rules from the sidecar scope
	// sidecarScope.config is nil if there is no sidecar scope for the namespace
	// TODO: This is a temporary gate until the sidecar implementation is stable. Once its stable, remove the
	// config != nil check
	if proxy != nil && proxy.SidecarScope != nil && proxy.SidecarScope.Config != nil && proxy.Type == SidecarProxy {
		// If there is a sidecar scope for this proxy, return the destination rule
		// from the sidecar scope.
		return proxy.SidecarScope.DestinationRule(service.Hostname)
	}

	// FIXME: this code should be removed once the EDS issue is fixed
	if proxy == nil {
		if host, ok := MostSpecificHostMatch(service.Hostname, ps.allExportedDestRules.hosts); ok {
			return ps.allExportedDestRules.destRule[host].config
		}
		return nil
	}

	// If the proxy config namespace is same as the root config namespace
	// look for dest rules in the service's namespace first. This hack is needed
	// because sometimes, istio-system tends to become the root config namespace.
	// Destination rules are defined here for global purposes. We dont want these
	// catch all destination rules to be the only dest rule, when processing CDS for
	// proxies like the istio-ingressgateway or istio-egressgateway.
	// If there are no service specific dest rules, we will end up picking up the same
	// rules anyway, later in the code
	if proxy.ConfigNamespace != ps.Env.Mesh.RootNamespace {
		// search through the DestinationRules in proxy's namespace first
		if ps.namespaceLocalDestRules[proxy.ConfigNamespace] != nil {
			if host, ok := MostSpecificHostMatch(service.Hostname,
				ps.namespaceLocalDestRules[proxy.ConfigNamespace].hosts); ok {
				return ps.namespaceLocalDestRules[proxy.ConfigNamespace].destRule[host].config
			}
		}
	}

	// if no private/public rule matched in the calling proxy's namespace,
	// check the target service's namespace for public rules
	if service.Attributes.Namespace != "" && ps.namespaceExportedDestRules[service.Attributes.Namespace] != nil {
		if host, ok := MostSpecificHostMatch(service.Hostname,
			ps.namespaceExportedDestRules[service.Attributes.Namespace].hosts); ok {
			return ps.namespaceExportedDestRules[service.Attributes.Namespace].destRule[host].config
		}
	}

	// if no public/private rule in calling proxy's namespace matched, and no public rule in the
	// target service's namespace matched, search for any public destination rule in the config root namespace
	// NOTE: This does mean that we are effectively ignoring private dest rules in the config root namespace
	if ps.namespaceExportedDestRules[ps.Env.Mesh.RootNamespace] != nil {
		if host, ok := MostSpecificHostMatch(service.Hostname,
			ps.namespaceExportedDestRules[ps.Env.Mesh.RootNamespace].hosts); ok {
			return ps.namespaceExportedDestRules[ps.Env.Mesh.RootNamespace].destRule[host].config
		}
	}

	return nil
}

// SubsetToLabels returns the labels associated with a subset of a given service.
func (ps *PushContext) SubsetToLabels(proxy *Proxy, subsetName string, hostname Hostname) LabelsCollection {
	// empty subset
	if subsetName == "" {
		return nil
	}

	config := ps.DestinationRule(proxy, &Service{Hostname: hostname})
	if config == nil {
		return nil
	}

	rule := config.Spec.(*networking.DestinationRule)
	for _, subset := range rule.Subsets {
		if subset.Name == subsetName {
			return []Labels{subset.Labels}
		}
	}

	return nil
}

// InitContext will initialize the data structures used for code generation.
// This should be called before starting the push, from the thread creating
// the push context.
func (ps *PushContext) InitContext(env *Environment) error {
	ps.Mutex.Lock()
	defer ps.Mutex.Unlock()
	if ps.initDone {
		return nil
	}
	ps.Env = env
	var err error

	// Must be initialized first
	// as initServiceRegistry/VirtualServices/Destrules
	// use the default export map
	ps.initDefaultExportMaps()

	if err = ps.initServiceRegistry(env); err != nil {
		return err
	}

	if err = ps.initVirtualServices(env); err != nil {
		return err
	}

	if err = ps.initDestinationRules(env); err != nil {
		return err
	}

	if err = ps.initAuthorizationPolicies(env); err != nil {
		rbacLog.Errorf("failed to initialize authorization policies: %v", err)
		return err
	}

	// Must be initialized in the end
	if err = ps.initSidecarScopes(env); err != nil {
		return err
	}

	ps.initDone = true
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
			if ps.defaultServiceExportTo[VisibilityPrivate] {
				ps.privateServicesByNamespace[ns] = append(ps.privateServicesByNamespace[ns], s)
			} else if ps.defaultServiceExportTo[VisibilityPublic] {
				ps.publicServices = append(ps.publicServices, s)
			}
		} else {
			if s.Attributes.ExportTo[VisibilityPrivate] {
				ps.privateServicesByNamespace[ns] = append(ps.privateServicesByNamespace[ns], s)
			} else {
				ps.publicServices = append(ps.publicServices, s)
			}
		}
		ps.ServiceByHostname[s.Hostname] = s
		ps.ServicePort2Name[string(s.Hostname)] = s.Ports
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
		ps.ServiceAccounts[svc.Hostname] = map[int][]string{}
		for _, port := range svc.Ports {
			if port.Protocol == ProtocolUDP {
				continue
			}
			ps.ServiceAccounts[svc.Hostname][port.Port] = env.GetIstioServiceAccounts(svc.Hostname, []int{port.Port})
		}
	}
}

// Caches list of virtual services
func (ps *PushContext) initVirtualServices(env *Environment) error {
	vservices, err := env.List(VirtualService.Type, NamespaceAll)
	if err != nil {
		return err
	}

	// TODO(rshriram): parse each virtual service and maintain a map of the
	// virtualservice name, the list of registry hosts in the VS and non
	// registry DNS names in the VS.  This should cut down processing in
	// the RDS code. See separateVSHostsAndServices in route/route.go
	sortConfigByCreationTime(vservices)

	// convert all shortnames in virtual services into FQDNs
	for _, r := range vservices {
		rule := r.Spec.(*networking.VirtualService)
		// resolve top level hosts
		for i, h := range rule.Hosts {
			rule.Hosts[i] = string(ResolveShortnameToFQDN(h, r.ConfigMeta))
		}
		// resolve gateways to bind to
		for i, g := range rule.Gateways {
			if g != IstioMeshGateway {
				rule.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
			}
		}
		// resolve host in http route.destination, route.mirror
		for _, d := range rule.Http {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range d.Route {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
			}
			if d.Mirror != nil {
				d.Mirror.Host = string(ResolveShortnameToFQDN(d.Mirror.Host, r.ConfigMeta))
			}
		}
		//resolve host in tcp route.destination
		for _, d := range rule.Tcp {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range d.Route {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
			}
		}
		//resolve host in tls route.destination
		for _, tls := range rule.Tls {
			for _, m := range tls.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = resolveGatewayName(g, r.ConfigMeta)
					}
				}
			}
			for _, w := range tls.Route {
				w.Destination.Host = string(ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta))
			}
		}
	}

	for _, virtualService := range vservices {
		ns := virtualService.Namespace
		rule := virtualService.Spec.(*networking.VirtualService)
		if len(rule.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			// TODO: We currently only honor ., * and ~
			if ps.defaultVirtualServiceExportTo[VisibilityPrivate] {
				// add to local namespace only
				ps.privateVirtualServicesByNamespace[ns] = append(ps.privateVirtualServicesByNamespace[ns], virtualService)
			} else if ps.defaultVirtualServiceExportTo[VisibilityPublic] {
				ps.publicVirtualServices = append(ps.publicVirtualServices, virtualService)
			}
		} else {
			// TODO: we currently only process the first element in the array
			// and currently only consider . or * which maps to public/private
			if Visibility(rule.ExportTo[0]) == VisibilityPrivate {
				// add to local namespace only
				ps.privateVirtualServicesByNamespace[ns] = append(ps.privateVirtualServicesByNamespace[ns], virtualService)
			} else {
				// ~ is not valid in the exportTo fields in virtualServices, services, destination rules
				// and we currently only allow . or *. So treat this as public export
				ps.publicVirtualServices = append(ps.publicVirtualServices, virtualService)
			}
		}
	}

	return nil
}

func (ps *PushContext) initDefaultExportMaps() {
	ps.defaultDestinationRuleExportTo = make(map[Visibility]bool)
	if ps.Env.Mesh.DefaultDestinationRuleExportTo != nil {
		for _, e := range ps.Env.Mesh.DefaultDestinationRuleExportTo {
			ps.defaultDestinationRuleExportTo[Visibility(e)] = true
		}
	} else {
		// default to *
		ps.defaultDestinationRuleExportTo[VisibilityPublic] = true
	}

	ps.defaultServiceExportTo = make(map[Visibility]bool)
	if ps.Env.Mesh.DefaultServiceExportTo != nil {
		for _, e := range ps.Env.Mesh.DefaultServiceExportTo {
			ps.defaultServiceExportTo[Visibility(e)] = true
		}
	} else {
		ps.defaultServiceExportTo[VisibilityPublic] = true
	}

	ps.defaultVirtualServiceExportTo = make(map[Visibility]bool)
	if ps.Env.Mesh.DefaultVirtualServiceExportTo != nil {
		for _, e := range ps.Env.Mesh.DefaultVirtualServiceExportTo {
			ps.defaultVirtualServiceExportTo[Visibility(e)] = true
		}
	} else {
		ps.defaultVirtualServiceExportTo[VisibilityPublic] = true
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
	sidecarConfigs, err := env.List(Sidecar.Type, NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(sidecarConfigs)

	ps.sidecarsByNamespace = make(map[string][]*SidecarScope)
	for _, sidecarConfig := range sidecarConfigs {
		// TODO: add entries with workloadSelectors first before adding namespace-wide entries
		sidecarConfig := sidecarConfig
		ps.sidecarsByNamespace[sidecarConfig.Namespace] = append(ps.sidecarsByNamespace[sidecarConfig.Namespace],
			ConvertToSidecarScope(ps, &sidecarConfig, sidecarConfig.Namespace))
	}

	// Hold reference root namespace's sidecar config
	// Root namespace can have only one sidecar config object
	// Currently we expect that it has no workloadSelectors
	var rootNSConfig *Config
	if env.Mesh.RootNamespace != "" {
		for _, sidecarConfig := range sidecarConfigs {
			if sidecarConfig.Namespace == env.Mesh.RootNamespace &&
				sidecarConfig.Spec.(*networking.Sidecar).WorkloadSelector == nil {
				rootNSConfig = &sidecarConfig
				break
			}
		}
	}

	// build sidecar scopes for other namespaces that dont have a sidecar CRD object.
	// Derive the sidecar scope from the root namespace's sidecar object if present. Else fallback
	// to the default Istio behavior mimicked by the DefaultSidecarScopeForNamespace function.
	for _, s := range ps.ServiceByHostname {
		ns := s.Attributes.Namespace
		if len(ps.sidecarsByNamespace[ns]) == 0 {
			// use the contents from the root namespace or the default if there is no root namespace
			ps.sidecarsByNamespace[ns] = []*SidecarScope{ConvertToSidecarScope(ps, rootNSConfig, ns)}
		}
	}

	return nil
}

// Split out of DestinationRule expensive conversions - once per push.
func (ps *PushContext) initDestinationRules(env *Environment) error {
	configs, err := env.List(DestinationRule.Type, NamespaceAll)
	if err != nil {
		return err
	}
	ps.SetDestinationRules(configs)
	return nil
}

// SetDestinationRules is updates internal structures using a set of configs.
// Split out of DestinationRule expensive conversions, computed once per push.
// This also allows tests to inject a config without having the mock.
func (ps *PushContext) SetDestinationRules(configs []Config) {
	// Sort by time first. So if two destination rule have top level traffic policies
	// we take the first one.
	sortConfigByCreationTime(configs)
	namespaceLocalDestRules := make(map[string]*processedDestRules)
	namespaceExportedDestRules := make(map[string]*processedDestRules)
	allExportedDestRules := &processedDestRules{
		hosts:    make([]Hostname, 0),
		destRule: map[Hostname]*combinedDestinationRule{},
	}

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
				hosts:    make([]Hostname, 0),
				destRule: map[Hostname]*combinedDestinationRule{},
			}
		}
		// Merge this destination rule with any public/private dest rules for same host in the same namespace
		// If there are no duplicates, the dest rule will be added to the list
		namespaceLocalDestRules[configs[i].Namespace].hosts, _ = ps.combineSingleDestinationRule(
			namespaceLocalDestRules[configs[i].Namespace].hosts,
			namespaceLocalDestRules[configs[i].Namespace].destRule,
			configs[i])

		isPubliclyExported := false
		if len(rule.ExportTo) == 0 {
			// No exportTo in destinationRule. Use the global default
			// TODO: We currently only honor ., * and ~
			if ps.defaultDestinationRuleExportTo[VisibilityPublic] {
				isPubliclyExported = true
			}
		} else {
			// TODO: we currently only process the first element in the array
			// and currently only consider . or * which maps to public/private
			if Visibility(rule.ExportTo[0]) != VisibilityPrivate {
				// ~ is not valid in the exportTo fields in virtualServices, services, destination rules
				// and we currently only allow . or *. So treat this as public export
				isPubliclyExported = true
			}
		}

		if isPubliclyExported {
			if _, exist := namespaceExportedDestRules[configs[i].Namespace]; !exist {
				namespaceExportedDestRules[configs[i].Namespace] = &processedDestRules{
					hosts:    make([]Hostname, 0),
					destRule: map[Hostname]*combinedDestinationRule{},
				}
			}
			// Merge this destination rule with any public dest rule for the same host in the same namespace
			// If there are no duplicates, the dest rule will be added to the list
			namespaceExportedDestRules[configs[i].Namespace].hosts, _ = ps.combineSingleDestinationRule(
				namespaceExportedDestRules[configs[i].Namespace].hosts,
				namespaceExportedDestRules[configs[i].Namespace].destRule,
				configs[i])

			// Merge this destination rule with any public dest rule for the same host
			// across all namespaces. If there are no duplicates, the dest rule will be added to the list
			allExportedDestRules.hosts, _ = ps.combineSingleDestinationRule(
				allExportedDestRules.hosts, allExportedDestRules.destRule, configs[i])
		}
	}

	// presort it so that we don't sort it for each DestinationRule call.
	// sort.Sort for Hostnames will automatically sort from the most specific to least specific
	for ns := range namespaceLocalDestRules {
		sort.Sort(Hostnames(namespaceLocalDestRules[ns].hosts))
	}
	for ns := range namespaceExportedDestRules {
		sort.Sort(Hostnames(namespaceExportedDestRules[ns].hosts))
	}
	sort.Sort(Hostnames(allExportedDestRules.hosts))

	ps.namespaceLocalDestRules = namespaceLocalDestRules
	ps.namespaceExportedDestRules = namespaceExportedDestRules
	ps.allExportedDestRules = allExportedDestRules
}

func (ps *PushContext) initAuthorizationPolicies(env *Environment) error {
	var err error
	if ps.AuthzPolicies, err = NewAuthzPolicies(env); err != nil {
		rbacLog.Errorf("failed to initialize authorization policies: %v", err)
		return err
	}
	return nil
}
