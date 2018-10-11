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
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"

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
	Mutex sync.Mutex `json:"-,omitempty"`

	// Services list all services in the system at the time push started.
	Services []*Service `json:"-,omitempty"`

	// ServiceByHostname has all services, indexed by hostname.
	ServiceByHostname map[Hostname]*Service `json:"-,omitempty"`

	//
	//ConfigsByType map[string][]*Config

	// TODO: add the remaining O(n**2) model, deprecate/remove all remaining
	// uses of model:

	//Endpoints map[string][]*ServiceInstance
	//ServicesForProxy map[string][]*ServiceInstance
	//ManagementPorts map[string]*PortList
	//WorkloadHealthCheck map[string]*ProbeList

	// ServiceAccounts represents the list of service accounts
	// for a service.
	//	ServiceAccounts map[string][]string
	// Temp: the code in alpha3 should use VirtualService directly
	VirtualServiceConfigs []Config `json:"-,omitempty"`

	destinationRuleHosts   []Hostname
	destinationRuleByHosts map[Hostname]*combinedDestinationRule

	//TODO: gateways              []*networking.Gateway

	// Env has a pointer to the shared environment used to create the snapshot.
	Env *Environment `json:"-,omitempty"`

	// ServiceAccounts is a function mapping service name to service accounts.
	ServiceAccounts func(string) []string `json:"-"`

	// ServicePort2Name is used to keep track of service name and port mapping.
	// This is needed because ADS names use port numbers, while endpoints use
	// port names. The key is the service name. If a service or port are not found,
	// the endpoint needs to be re-evaluated later (eventual consistency)
	ServicePort2Name map[string]PortList `json:"-"`

	initDone bool
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
// reduce the time when subsetting or split-horizon is used. This desing assumes pilot
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
}

// IstioEndpoint has the information about a single address+port for a specific
// service and shard.
//
// Replaces NetworkEndpoint and ServiceInstance:
// - ServicePortName replaces ServicePort, since port number and protocol may not
// be available when endpoint callbacks are made.
// - It no longers splits into one ServiceInstance and one NetworkEndpoint - both
// are in a single struct
// - doesn't have a pointer to Service - the full Service object may not be available at
// the time the endpoint is received. The service name is used as a key and used to reconcile.
// - it has a cached EnvoyEndpoint object - to avoid re-allocating it for each request and
// client.
type IstioEndpoint struct {

	// Labels points to the workload or deployment labels.
	Labels map[string]string

	// Family indicates what type of endpoint, such as TCP or Unix Domain Socket.
	// Default is TCP.
	Family AddressFamily

	// Address is the address of the endpoint, using envoy proto.
	Address string

	// EndpointPort is the port where the workload is listening, can be different
	// from the service port.
	EndpointPort uint32

	// ServicePortName tracks the name of the port, to avoid 'eventual consistency' issues.
	// Sometimes the Endpoint is visible before Service - so looking up the port number would
	// fail. Instead the mapping to number is made when the clusters are computed. The lazy
	// computation will also help with 'on-demand' and 'split horizon' - where it will be skipped
	// for not used clusters or endpoints behind a gate.
	ServicePortName string

	// UID identifies the workload, for telemetry purpose.
	UID string

	// EnvoyEndpoint is a cached LbEndpoint, converted from the data, to
	// avoid recomputation
	EnvoyEndpoint *endpoint.LbEndpoint

	// ServiceAccount holds the associated service account.
	// May be replaced once a better optimization/struct is available for secure naming.
	ServiceAccount string
}

// ServiceShards holds the set of endpoint shards of a service. Registries update
// individual shards incrementally. The shards are aggregated and split into
// clusters when a push for the specific cluster is needed.
type ServiceShards struct {

	// Shards is used to track the shards. EDS updates are grouped by shard.
	// Current implementation uses the registry name as key - in multicluster this is the
	// name of the k8s cluster, derived from the config (secret).
	Shards map[string]*EndpointShard

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts map[string]bool
}

// EndpointShard contains all the endpoints for a single shard (subset) of a service.
// Shards are updated atomically by registries. A registry may split a service into
// multiple shards (for example each deployment, or smaller sub-sets).
type EndpointShard struct {
	Shard   string
	Entries []*IstioEndpoint
}

// ConfigUpdater is used to requests config updates. The updates will be debounced before
// a push happens. Interface is implemented by the ADS server, and called by adapters.
type ConfigUpdater interface {
	// Push is called the global config has changed and a full push is needed.
	// This replaces the 'cache invalidation' model. The implementation may debounce and
	// batch changes.
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
	subsets map[string]bool // list of subsets seen so far
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

	// All metrics we registered.
	metrics []*PushMetric
)

// NewPushContext creates a new PushContext structure to track push status.
func NewPushContext() *PushContext {
	// TODO: detect push in progress, don't update status if set
	return &PushContext{
		ServiceByHostname: map[Hostname]*Service{},
		ProxyStatus:       map[string]map[string]ProxyPushStatus{},
		ServicePort2Name:  map[string]PortList{},
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
	LastPushStatus = ps
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

// VirtualServices lists all virtual services bound to the specified gateways
// This replaces store.VirtualServices
func (ps *PushContext) VirtualServices(gateways map[string]bool) []Config {
	configs := ps.VirtualServiceConfigs
	out := make([]Config, 0)
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
				if gateways[ResolveShortnameToFQDN(g, config.ConfigMeta).String()] {
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
	if err = ps.initServiceRegistry(env); err != nil {
		return err
	}

	if err = ps.initVirtualServices(env); err != nil {
		return err
	}

	if err = ps.initDestinationRules(env); err != nil {
		return err
	}

	// TODO: everything else that is used in config generation - the generation
	// should not have any deps on config store.
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
	ps.Services = sortServicesByCreationTime(services)
	for _, s := range services {
		ps.ServiceByHostname[s.Hostname] = s
		ps.ServicePort2Name[string(s.Hostname)] = s.Ports
	}
	return nil
}

// sortServicesByCreationTime sorts the list of services in ascending order by their creation time (if available).
func sortServicesByCreationTime(services []*Service) []*Service {
	sort.SliceStable(services, func(i, j int) bool {
		return services[i].CreationTime.Before(services[j].CreationTime)
	})
	return services
}

// Caches list of virtual services
func (ps *PushContext) initVirtualServices(env *Environment) error {
	vservices, err := env.List(VirtualService.Type, NamespaceAll)
	if err != nil {
		return err
	}

	sortConfigByCreationTime(vservices)
	ps.VirtualServiceConfigs = vservices
	// convert all shortnames in virtual services into FQDNs
	for _, r := range ps.VirtualServiceConfigs {
		rule := r.Spec.(*networking.VirtualService)
		// resolve top level hosts
		for i, h := range rule.Hosts {
			rule.Hosts[i] = ResolveShortnameToFQDN(h, r.ConfigMeta).String()
		}
		// resolve gateways to bind to
		for i, g := range rule.Gateways {
			if g != IstioMeshGateway {
				rule.Gateways[i] = ResolveShortnameToFQDN(g, r.ConfigMeta).String()
			}
		}
		// resolve host in http route.destination, route.mirror
		for _, d := range rule.Http {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = ResolveShortnameToFQDN(g, r.ConfigMeta).String()
					}
				}
			}
			for _, w := range d.Route {
				w.Destination.Host = ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta).String()
			}
			if d.Mirror != nil {
				d.Mirror.Host = ResolveShortnameToFQDN(d.Mirror.Host, r.ConfigMeta).String()
			}
		}
		//resolve host in tcp route.destination
		for _, d := range rule.Tcp {
			for _, m := range d.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = ResolveShortnameToFQDN(g, r.ConfigMeta).String()
					}
				}
			}
			for _, w := range d.Route {
				w.Destination.Host = ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta).String()
			}
		}
		//resolve host in tls route.destination
		for _, tls := range rule.Tls {
			for _, m := range tls.Match {
				for i, g := range m.Gateways {
					if g != IstioMeshGateway {
						m.Gateways[i] = ResolveShortnameToFQDN(g, r.ConfigMeta).String()
					}
				}
			}
			for _, w := range tls.Route {
				w.Destination.Host = ResolveShortnameToFQDN(w.Destination.Host, r.ConfigMeta).String()
			}
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
	hosts := make([]Hostname, 0)
	combinedDestinationRuleMap := make(map[Hostname]*combinedDestinationRule, len(configs))

	for i := range configs {
		rule := configs[i].Spec.(*networking.DestinationRule)
		resolvedHost := ResolveShortnameToFQDN(rule.Host, configs[i].ConfigMeta)
		if mdr, exists := combinedDestinationRuleMap[resolvedHost]; exists {
			combinedRule := mdr.config.Spec.(*networking.DestinationRule)
			// we have an another destination rule for same host.
			// concatenate both of them -- essentially add subsets from one to other.
			for _, subset := range rule.Subsets {
				if _, subsetExists := mdr.subsets[subset.Name]; !subsetExists {
					mdr.subsets[subset.Name] = true
					combinedRule.Subsets = append(combinedRule.Subsets, subset)
				} else {
					ps.Add(DuplicatedSubsets, string(resolvedHost), nil,
						fmt.Sprintf("Duplicate subset %s found while merging destination rules for %s",
							subset.Name, string(resolvedHost)))
				}

				// If there is no top level policy and the incoming rule has top level
				// traffic policy, use the one from the incoming rule.
				if combinedRule.TrafficPolicy == nil && rule.TrafficPolicy != nil {
					combinedRule.TrafficPolicy = rule.TrafficPolicy
				}
			}
			continue
		}

		combinedDestinationRuleMap[resolvedHost] = &combinedDestinationRule{
			subsets: make(map[string]bool),
			config:  &configs[i],
		}
		for _, subset := range rule.Subsets {
			combinedDestinationRuleMap[resolvedHost].subsets[subset.Name] = true
		}
		hosts = append(hosts, resolvedHost)
	}

	// presort it so that we don't sort it for each DestinationRule call.
	sort.Sort(Hostnames(hosts))
	ps.destinationRuleHosts = hosts
	ps.destinationRuleByHosts = combinedDestinationRuleMap
}

// DestinationRule returns a destination rule for a service name in a given domain.
func (ps *PushContext) DestinationRule(hostname Hostname) *Config {
	if c, ok := MostSpecificHostMatch(hostname, ps.destinationRuleHosts); ok {
		return ps.destinationRuleByHosts[c].config
	}
	return nil
}

// SubsetToLabels returns the labels associated with a subset of a given service.
func (ps *PushContext) SubsetToLabels(subsetName string, hostname Hostname) LabelsCollection {
	// empty subset
	if subsetName == "" {
		return nil
	}

	config := ps.DestinationRule(hostname)
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
