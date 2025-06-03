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
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const (
	wildcardNamespace = "*"
	currentNamespace  = "."
	wildcardService   = host.Name("*")
)

var (
	sidecarScopedKnownConfigTypes = sets.New(
		kind.ServiceEntry,
		kind.VirtualService,
		kind.DestinationRule,
		kind.Sidecar,
	)

	// clusterScopedKnownConfigTypes includes configs when they are in root namespace,
	// they will be applied to all namespaces within the cluster.
	clusterScopedKnownConfigTypes = sets.New(
		kind.EnvoyFilter,
		kind.AuthorizationPolicy,
		kind.RequestAuthentication,
		kind.PeerAuthentication,
		kind.WasmPlugin,
		kind.Telemetry,
	)
)

type hostClassification struct {
	exactHosts sets.Set[host.Name]
	allHosts   []host.Name
}

// Matches checks if the hostClassification(sidecar egress hosts) matches the Service's hostname
func (hc hostClassification) Matches(h host.Name) bool {
	// exact lookup is fast, so check that first
	if hc.exactHosts.Contains(h) {
		return true
	}

	// exactHosts not found, fallback to loop allHosts
	hIsWildCarded := h.IsWildCarded()
	for _, importedHost := range hc.allHosts {
		// If both are exact hosts, then fallback is not needed.
		// In this scenario it should be determined by exact lookup.
		if !hIsWildCarded && !importedHost.IsWildCarded() {
			continue
		}
		// Check if the hostnames match per usual hostname matching rules
		if h.SubsetOf(importedHost) {
			return true
		}
	}
	return false
}

// VSMatches checks if the hostClassification(sidecar egress hosts) matches the VirtualService's host
func (hc hostClassification) VSMatches(vsHost host.Name, useGatewaySemantics bool) bool {
	// first, check exactHosts
	if hc.exactHosts.Contains(vsHost) {
		return true
	}

	// exactHosts not found, fallback to loop allHosts
	hIsWildCard := vsHost.IsWildCarded()
	for _, importedHost := range hc.allHosts {
		// If both are exact hosts, then fallback is not needed.
		// In this scenario it should be determined by exact lookup.
		if !hIsWildCard && !importedHost.IsWildCarded() {
			continue
		}

		var match bool
		if useGatewaySemantics {
			// The new way. Matching logic exactly mirrors Service matching
			// If a route defines `*.com` and we import `a.com`, it will not match
			match = vsHost.SubsetOf(importedHost)
		} else {
			// The old way. We check Matches which is bi-directional. This is for backwards compatibility
			match = vsHost.Matches(importedHost)
		}
		if match {
			return true
		}
	}
	return false
}

// SidecarScope is a wrapper over the Sidecar resource with some
// preprocessed data to determine the list of services, virtualServices,
// and destinationRules that are accessible to a given
// sidecar. Precomputing the list of services, virtual services, dest rules
// for a sidecar improves performance as we no longer need to compute this
// list for every sidecar. We simply have to match a sidecar to a
// SidecarScope. Note that this is not the same as public/private scoped
// services. The list of services seen by every sidecar scope (namespace
// wide or per workload) depends on the imports, the listeners, and other
// settings.
//
// Every proxy workload of SidecarProxy type will always map to a
// SidecarScope object. If the proxy's namespace does not have a user
// specified Sidecar CRD, we will construct one that has a catch all egress
// listener that imports every public service/virtualService in the mesh.
type SidecarScope struct {
	Name string
	// This is the namespace where the sidecar takes effect,
	// maybe different from the ns where sidecar resides if sidecar is in root ns.
	Namespace string
	// The cr itself. Can be nil if we are constructing the default
	// sidecar scope
	Sidecar *networking.Sidecar

	// Version this sidecar was computed for
	Version string

	// Set of egress listeners, and their associated services.  A sidecar
	// scope should have either ingress/egress listeners or both.  For
	// every proxy workload that maps to a sidecar API object (or the
	// default object), we will go through every egress listener in the
	// object and process the Envoy listener or RDS based on the imported
	// services/virtual services in that listener.
	EgressListeners []*IstioEgressListenerWrapper

	// Union of services imported across all egress listeners for use by CDS code.
	services           []*Service
	servicesByHostname map[host.Name]*Service

	// Destination rules imported across all egress listeners. This
	// contains the computed set based on public/private destination rules
	// as well as the inherited ones, in addition to the wildcard matches
	// such as *.com applying to foo.bar.com. Each hostname in this map
	// corresponds to a service in the services array above. When computing
	// CDS, we simply have to find the matching service and return the
	// destination rule.
	destinationRules        map[host.Name][]*ConsolidatedDestRule
	destinationRulesByNames map[types.NamespacedName]*config.Config

	// OutboundTrafficPolicy defines the outbound traffic policy for this sidecar.
	// If OutboundTrafficPolicy is ALLOW_ANY traffic to unknown destinations will
	// be forwarded.
	OutboundTrafficPolicy *networking.OutboundTrafficPolicy

	// Set of known configs this sidecar depends on.
	// This field will be used to determine the config/resource scope
	// which means which config changes will affect the proxies within this scope.
	configDependencies sets.Set[ConfigHash]

	// Function that will initialize the sidecar scope. This is used to
	// defer the initialization of the sidecar scope until the first time
	// it is used
	initFunc func()
}

// MarshalJSON implements json.Marshaller
func (sc *SidecarScope) MarshalJSON() ([]byte, error) {
	// Json cannot expose unexported fields, so copy the ones we want here
	return json.MarshalIndent(map[string]any{
		"version":               sc.Version,
		"name":                  sc.Name,
		"namespace":             sc.Namespace,
		"outboundTrafficPolicy": sc.OutboundTrafficPolicy,
		"services":              sc.services,
		"egressListenerServices": slices.Map(sc.EgressListeners, func(e *IstioEgressListenerWrapper) []*Service {
			return e.services
		}),
		"servicesByHostname": sc.servicesByHostname,
		"sidecar":            sc.Sidecar,
		"destinationRules":   sc.destinationRules,
	}, "", "  ")
}

// IstioEgressListenerWrapper is a wrapper for
// networking.IstioEgressListener object. The wrapper provides performance
// optimizations as it allows us to precompute and store the list of
// services/virtualServices that apply to this listener.
type IstioEgressListenerWrapper struct {
	// The actual IstioEgressListener api object from the Config. It can be
	// nil if this is for the default sidecar scope.
	IstioListener *networking.IstioEgressListener

	// Specifies whether matching ports is required.
	matchPort bool

	// List of services imported by this egress listener above.
	// This will be used by LDS and RDS code when
	// building the set of virtual hosts or the tcp filterchain matches for
	// a given listener port. Two listeners, on user specified ports or
	// unix domain sockets could have completely different sets of
	// services. So a global list of services per sidecar scope will be
	// incorrect. Hence the per listener set of services.
	services []*Service

	// List of virtual services imported by this egress listener above.
	// As with per listener services, this
	// will be used by RDS code to compute the virtual host configs for
	// http listeners, as well as by TCP/TLS filter code to compute the
	// service routing configs and the filter chain matches. We need a
	// virtualService set per listener and not one per sidecarScope because
	// each listener imports an independent set of virtual services.
	// Listener 1 could import a public virtual service for serviceA from
	// namespace A that has some path rewrite, while listener2 could import
	// a private virtual service for serviceA from the local namespace,
	// with a different path rewrite or no path rewrites.
	virtualServices []config.Config

	// An index of hostname to the namespaced name of the VirtualService containing the most
	// specific host match.
	mostSpecificWildcardVsIndex map[host.Name]types.NamespacedName
}

const defaultSidecar = "default-sidecar"

// DefaultSidecarScopeForGateway builds a SidecarScope contains services and destinationRules for a given gateway/waypoint.
func DefaultSidecarScopeForGateway(ps *PushContext, configNamespace string) *SidecarScope {
	services := ps.servicesExportedToNamespace(configNamespace)
	out := &SidecarScope{
		Name:                    defaultSidecar,
		Namespace:               configNamespace,
		destinationRules:        make(map[host.Name][]*ConsolidatedDestRule),
		destinationRulesByNames: make(map[types.NamespacedName]*config.Config),
		servicesByHostname:      make(map[host.Name]*Service, len(services)),
		Version:                 ps.PushVersion,
	}

	servicesAdded := make(map[host.Name]sidecarServiceIndex)
	for _, s := range services {
		out.appendSidecarServices(servicesAdded, s)
	}
	out.selectDestinationRules(ps, configNamespace)

	// waypoint need to get vses from the egress listener
	defaultEgressListener := &IstioEgressListenerWrapper{
		virtualServices: ps.VirtualServicesForGateway(configNamespace, constants.IstioMeshGateway),
	}
	out.EgressListeners = []*IstioEgressListenerWrapper{defaultEgressListener}
	out.initFunc = func() {}

	return out
}

// DefaultSidecarScopeForNamespace is a sidecar scope object with a default catch all egress listener
// that matches the default Istio behavior: a sidecar has listeners for all services in the mesh
// We use this scope when the user has not set any sidecar Config for a given config namespace.
func DefaultSidecarScopeForNamespace(ps *PushContext, configNamespace string) *SidecarScope {
	if features.UnifiedSidecarScoping {
		// Modern way: treat no Sidecar the same as having a Sidecar with a single egress listener for `*/*`
		return convertToSidecarScope(ps, nil, configNamespace)
	}
	// Legacy way, for compatibility: disjoint logic for Sidecar vs no Sidecar. This has minor differences in the face of
	// overlapping hostnames across services.
	defaultEgressListener := &IstioEgressListenerWrapper{
		IstioListener: &networking.IstioEgressListener{
			Hosts: []string{"*/*"},
		},
	}
	services := ps.servicesExportedToNamespace(configNamespace)
	defaultEgressListener.virtualServices = ps.VirtualServicesForGateway(configNamespace, constants.IstioMeshGateway)
	defaultEgressListener.mostSpecificWildcardVsIndex = computeWildcardHostVirtualServiceIndex(
		defaultEgressListener.virtualServices, services)

	out := &SidecarScope{
		Name:                    defaultSidecar,
		Namespace:               configNamespace,
		EgressListeners:         []*IstioEgressListenerWrapper{defaultEgressListener},
		destinationRules:        make(map[host.Name][]*ConsolidatedDestRule),
		destinationRulesByNames: make(map[types.NamespacedName]*config.Config),
		servicesByHostname:      make(map[host.Name]*Service, len(defaultEgressListener.services)),
		configDependencies:      make(sets.Set[ConfigHash]),
		Version:                 ps.PushVersion,
	}

	servicesAdded := make(map[host.Name]sidecarServiceIndex)
	for _, s := range services {
		out.appendSidecarServices(servicesAdded, s)
	}
	defaultEgressListener.services = out.services

	// add dependencies on delegate virtual services
	delegates := ps.DelegateVirtualServices(defaultEgressListener.virtualServices)
	for _, delegate := range delegates {
		out.AddConfigDependencies(delegate)
	}
	for _, vs := range defaultEgressListener.virtualServices {
		out.AddConfigDependencies(ConfigKey{
			Kind:      kind.VirtualService,
			Namespace: vs.Namespace,
			Name:      vs.Name,
		}.HashCode())
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	for _, s := range out.services {
		if dr := ps.destinationRule(configNamespace, s); dr != nil {
			out.destinationRules[s.Hostname] = dr
			for _, cdr := range dr {
				for _, from := range cdr.from {
					out.destinationRulesByNames[from] = cdr.rule
					out.AddConfigDependencies(ConfigKey{
						Kind:      kind.DestinationRule,
						Name:      from.Name,
						Namespace: from.Namespace,
					}.HashCode())
				}
			}
		}
		out.AddConfigDependencies(ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(s.Hostname),
			Namespace: s.Attributes.Namespace,
		}.HashCode())
	}

	if ps.Mesh.OutboundTrafficPolicy != nil {
		out.OutboundTrafficPolicy = &networking.OutboundTrafficPolicy{
			Mode: networking.OutboundTrafficPolicy_Mode(ps.Mesh.OutboundTrafficPolicy.Mode),
		}
	}

	out.initFunc = func() {}

	return out
}

// convertToSidecarScope converts from Sidecar config to SidecarScope object
func convertToSidecarScope(ps *PushContext, sidecarConfig *config.Config, configNamespace string) *SidecarScope {
	out := &SidecarScope{
		Name:               defaultSidecar,
		Namespace:          configNamespace,
		servicesByHostname: make(map[host.Name]*Service),
		configDependencies: make(sets.Set[ConfigHash]),
		Version:            ps.PushVersion,
	}
	var sidecar *networking.Sidecar
	if sidecarConfig != nil {
		sidecar = sidecarConfig.Spec.(*networking.Sidecar)
		out.Sidecar = sidecar
		out.Name = sidecarConfig.Name
		out.AddConfigDependencies(ConfigKey{
			Kind:      kind.Sidecar,
			Name:      sidecarConfig.Name,
			Namespace: sidecarConfig.Namespace,
		}.HashCode())

	}

	out.initFunc = sync.OnceFunc(func() {
		initSidecarScopeInternalIndexes(ps, out, configNamespace)
	})

	if !features.EnableLazySidecarEvaluation {
		out.initFunc()
	}

	return out
}

// initSidecarScopeInternalIndexes initializes SidecarScope's internal indexes derived from PushContext
func initSidecarScopeInternalIndexes(ps *PushContext, sidecarScope *SidecarScope, configNamespace string) {
	egressConfigs := sidecarScope.Sidecar.GetEgress()
	// If egress not set, setup a default listener
	if len(egressConfigs) == 0 {
		egressConfigs = append(egressConfigs, &networking.IstioEgressListener{Hosts: []string{"*/*"}})
	}
	sidecarScope.EgressListeners = make([]*IstioEgressListenerWrapper, 0, len(egressConfigs))
	for _, e := range egressConfigs {
		sidecarScope.EgressListeners = append(sidecarScope.EgressListeners,
			convertIstioListenerToWrapper(ps, configNamespace, e))
	}

	// Now collect all the imported services across all egress listeners in
	// this sidecar crd. This is needed to generate CDS output
	sidecarScope.collectImportedServices(ps, configNamespace)

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	sidecarScope.selectDestinationRules(ps, configNamespace)

	if sidecarScope.Sidecar.GetOutboundTrafficPolicy() == nil {
		if ps.Mesh.OutboundTrafficPolicy != nil {
			sidecarScope.OutboundTrafficPolicy = &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_Mode(ps.Mesh.OutboundTrafficPolicy.Mode),
			}
		}
	} else {
		sidecarScope.OutboundTrafficPolicy = sidecarScope.Sidecar.GetOutboundTrafficPolicy()
	}
}

func (sc *SidecarScope) collectImportedServices(ps *PushContext, configNamespace string) {
	serviceMatchingPort := func(s *Service, ilw *IstioEgressListenerWrapper, ports sets.Set[int]) *Service {
		if ilw.matchPort {
			return serviceMatchingListenerPort(s, ilw)
		}
		return serviceMatchingVirtualServicePorts(s, ports)
	}

	servicesAdded := make(map[host.Name]sidecarServiceIndex)
	for _, ilw := range sc.EgressListeners {
		// First add the explicitly requested services, which take priority
		for _, s := range ilw.services {
			sc.appendSidecarServices(servicesAdded, s)
		}

		// add dependencies on delegate virtual services
		delegates := ps.DelegateVirtualServices(ilw.virtualServices)
		sc.AddConfigDependencies(delegates...)

		// Infer more possible destinations from virtual services
		// Services chosen here will not override services explicitly requested in ilw.services.
		// That way, if there is ambiguity around what hostname to pick, a user can specify the one they
		// want in the hosts field, and the potentially random choice below won't matter
		for _, vs := range ilw.virtualServices {
			sc.AddConfigDependencies(ConfigKey{
				Kind:      kind.VirtualService,
				Namespace: vs.Namespace,
				Name:      vs.Name,
			}.HashCode())
			v := vs.Spec.(*networking.VirtualService)
			for h, ports := range virtualServiceDestinationsFilteredBySourceNamespace(v, configNamespace) {
				byNamespace := ps.ServiceIndex.HostnameAndNamespace[host.Name(h)]
				// Default to this hostname in our config namespace
				if s, ok := byNamespace[configNamespace]; ok {
					// This won't overwrite hostnames that have already been found eg because they were requested in hosts
					if matchedSvc := serviceMatchingPort(s, ilw, ports); matchedSvc != nil {
						sc.appendSidecarServices(servicesAdded, matchedSvc)
					}
				} else {
					// We couldn't find the hostname in our config namespace
					// We have to pick one arbitrarily for now, so we'll pick the first namespace alphabetically
					// TODO: could we choose services more intelligently based on their ports?
					if len(byNamespace) == 0 {
						// This hostname isn't found anywhere
						log.Debugf("Could not find service hostname %s parsed from %s", h, vs.Key())
						continue
					}
					// This won't overwrite hostnames that have already been found eg because they were requested in hosts
					if ns := pickFirstVisibleNamespace(ps, byNamespace, configNamespace); ns != "" {
						if matchedSvc := serviceMatchingPort(byNamespace[ns], ilw, ports); matchedSvc != nil {
							sc.appendSidecarServices(servicesAdded, matchedSvc)
						}
					}
				}
			}
		}
	}
}

func (sc *SidecarScope) selectDestinationRules(ps *PushContext, configNamespace string) {
	sc.destinationRules = make(map[host.Name][]*ConsolidatedDestRule)
	sc.destinationRulesByNames = make(map[types.NamespacedName]*config.Config)
	for _, s := range sc.services {
		drList := ps.destinationRule(configNamespace, s)
		if drList != nil {
			sc.destinationRules[s.Hostname] = drList
			for _, dr := range drList {
				for _, key := range dr.from {
					sc.AddConfigDependencies(ConfigKey{
						Kind:      kind.DestinationRule,
						Name:      key.Name,
						Namespace: key.Namespace,
					}.HashCode())

					sc.destinationRulesByNames[key] = dr.rule
				}
			}
		}
		sc.AddConfigDependencies(ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(s.Hostname),
			Namespace: s.Attributes.Namespace,
		}.HashCode())
	}
}

func convertIstioListenerToWrapper(ps *PushContext, configNamespace string,
	istioListener *networking.IstioEgressListener,
) *IstioEgressListenerWrapper {
	out := &IstioEgressListenerWrapper{
		IstioListener: istioListener,
		matchPort:     needsPortMatch(istioListener),
	}

	hostsByNamespace := make(map[string]hostClassification)
	for _, h := range istioListener.Hosts {
		if strings.Count(h, "/") != 1 {
			log.Errorf("Illegal host in sidecar resource: %s, host must be of form namespace/dnsName", h)
			continue
		}
		ns, name, _ := strings.Cut(h, "/")
		if ns == currentNamespace {
			ns = configNamespace
		}

		hName := host.Name(name)
		hc, exists := hostsByNamespace[ns]
		if !exists {
			hc = hostClassification{exactHosts: sets.New[host.Name](), allHosts: make([]host.Name, 0)}
		}

		// exact hosts are saved separately for map lookup
		if !hName.IsWildCarded() {
			hc.exactHosts.Insert(hName)
		}

		// allHosts contains the exact hosts and wildcard hosts,
		// since SelectVirtualServices will use `Matches` semantic matching.
		hc.allHosts = append(hc.allHosts, hName)
		hostsByNamespace[ns] = hc
	}

	out.virtualServices = SelectVirtualServices(ps.virtualServiceIndex, configNamespace, hostsByNamespace)
	svces := ps.servicesExportedToNamespace(configNamespace)
	out.services = out.selectServices(svces, configNamespace, hostsByNamespace)
	out.mostSpecificWildcardVsIndex = computeWildcardHostVirtualServiceIndex(out.virtualServices, out.services)

	return out
}

// GetEgressListenerForRDS returns the egress listener corresponding to
// the listener port or the bind address or the catch all listener
func (sc *SidecarScope) GetEgressListenerForRDS(port int, bind string) *IstioEgressListenerWrapper {
	if sc == nil {
		return nil
	}

	for _, e := range sc.EgressListeners {
		// We hit a catchall listener. This is the last listener in the list of listeners
		// return as is
		if e.IstioListener == nil || e.IstioListener.Port == nil {
			return e
		}

		// Check if the ports match
		// for unix domain sockets (i.e. port == 0), check if the bind is equal to the routeName
		if int(e.IstioListener.Port.Number) == port {
			if port == 0 { // unix domain socket
				if e.IstioListener.Bind == bind {
					return e
				}
				// no match.. continue searching
				continue
			}
			// this is a non-zero port match
			return e
		}
	}

	// This should never be reached unless user explicitly set an empty array for egress
	// listeners which we actually forbid
	return nil
}

// HasIngressListener returns if the sidecar scope has ingress listener set
func (sc *SidecarScope) HasIngressListener() bool {
	if sc == nil {
		return false
	}

	if sc.Sidecar == nil || len(sc.Sidecar.Ingress) == 0 {
		return false
	}

	return true
}

// InboundConnectionPoolForPort returns the connection pool settings for a specific inbound port. If there's not a
// setting for that specific port, then the settings at the Sidecar resource are returned. If neither exist,
// then nil is returned so the caller can decide what values to fall back on.
func (sc *SidecarScope) InboundConnectionPoolForPort(port int) *networking.ConnectionPoolSettings {
	if sc == nil || sc.Sidecar == nil {
		return nil
	}

	for _, in := range sc.Sidecar.Ingress {
		if int(in.Port.Number) == port {
			if in.GetConnectionPool() != nil {
				return in.ConnectionPool
			}
		}
	}

	// if set, it'll be non-nil and have values (guaranteed by validation); or if unset it'll be nil
	return sc.Sidecar.GetInboundConnectionPool()
}

// Services returns the list of services imported by this egress listener
func (ilw *IstioEgressListenerWrapper) Services() []*Service {
	return ilw.services
}

// VirtualServices returns the list of virtual services imported by this
// egress listener
func (ilw *IstioEgressListenerWrapper) VirtualServices() []config.Config {
	return ilw.virtualServices
}

// MostSpecificWildcardVirtualServiceIndex returns the mostSpecificWildcardVsIndex for this egress
// listener.
func (ilw *IstioEgressListenerWrapper) MostSpecificWildcardVirtualServiceIndex() map[host.Name]types.NamespacedName {
	return ilw.mostSpecificWildcardVsIndex
}

// DependsOnConfig determines if the proxy depends on the given config.
// Returns whether depends on this config or this kind of config is not scopeZd(unknown to be depended) here.
func (sc *SidecarScope) DependsOnConfig(config ConfigKey, rootNs string) bool {
	if sc == nil {
		return true
	}

	// This kind of config will trigger a change if made in the root namespace or the same namespace
	if clusterScopedKnownConfigTypes.Contains(config.Kind) {
		return config.Namespace == rootNs || config.Namespace == sc.Namespace
	}

	// This kind of config is unknown to sidecarScope.
	if _, f := sidecarScopedKnownConfigTypes[config.Kind]; !f {
		return true
	}

	return sc.configDependencies.Contains(config.HashCode())
}

func (sc *SidecarScope) GetService(hostname host.Name) *Service {
	if sc == nil {
		return nil
	}
	return sc.servicesByHostname[hostname]
}

// AddConfigDependencies add extra config dependencies to this scope. This action should be done before the
// SidecarScope being used to avoid concurrent read/write.
func (sc *SidecarScope) AddConfigDependencies(dependencies ...ConfigHash) {
	if sc == nil {
		return
	}
	if sc.configDependencies == nil {
		sc.configDependencies = sets.New(dependencies...)
	} else {
		sc.configDependencies.InsertAll(dependencies...)
	}
}

// DestinationRule returns a destinationrule for a svc.
func (sc *SidecarScope) DestinationRule(direction TrafficDirection, proxy *Proxy, svc host.Name) *ConsolidatedDestRule {
	destinationRules := sc.destinationRules[svc]
	var catchAllDr *ConsolidatedDestRule
	for _, destRule := range destinationRules {
		destinationRule := destRule.rule.Spec.(*networking.DestinationRule)
		if destinationRule.GetWorkloadSelector() == nil {
			catchAllDr = destRule
		}
		// filter DestinationRule based on workloadSelector for outbound configs.
		// WorkloadSelector configuration is honored only for outbound configuration, because
		// for inbound configuration, the settings at sidecar would be more explicit and the preferred way forward.
		if sc.Namespace == destRule.rule.Namespace &&
			destinationRule.GetWorkloadSelector() != nil && direction == TrafficDirectionOutbound {
			workloadSelector := labels.Instance(destinationRule.GetWorkloadSelector().GetMatchLabels())
			// return destination rule if workload selector matches
			if workloadSelector.SubsetOf(proxy.Labels) {
				return destRule
			}
		}
	}
	// If there is no workload specific destinationRule, return the wild carded dr if present.
	if catchAllDr != nil {
		return catchAllDr
	}
	return nil
}

// DestinationRuleConfig returns merged destination rules for a svc.
func (sc *SidecarScope) DestinationRuleConfig(direction TrafficDirection, proxy *Proxy, svc host.Name) *config.Config {
	cdr := sc.DestinationRule(direction, proxy, svc)
	if cdr == nil {
		return nil
	}
	return cdr.rule
}

// Services returns the list of services that are visible to a sidecar.
func (sc *SidecarScope) Services() []*Service {
	return sc.services
}

// Testing Only. This allows tests to inject a config without having the mock.
func (sc *SidecarScope) SetDestinationRulesForTesting(configs []config.Config) {
	sc.destinationRulesByNames = make(map[types.NamespacedName]*config.Config)
	for _, c := range configs {
		sc.destinationRulesByNames[types.NamespacedName{Name: c.Name, Namespace: c.Namespace}] = &c
	}
}

func (sc *SidecarScope) DestinationRuleByName(name, namespace string) *config.Config {
	if sc == nil {
		return nil
	}
	return sc.destinationRulesByNames[types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}]
}

// ServicesForHostname returns a list of services that fall under the hostname provided. This hostname
// can be a wildcard.
func (sc *SidecarScope) ServicesForHostname(hostname host.Name) []*Service {
	if !hostname.IsWildCarded() {
		if svc, f := sc.servicesByHostname[hostname]; f {
			return []*Service{svc}
		}
		return nil
	}
	services := make([]*Service, 0)
	for _, svc := range sc.services {
		if hostname.Matches(svc.Hostname) {
			services = append(services, svc)
		}
	}
	return services
}

// Return filtered services through the hosts field in the egress portion of the Sidecar config.
// Note that the returned service could be trimmed.
// TODO: support merging services within this egress listener to align with SidecarScope's behavior.
func (ilw *IstioEgressListenerWrapper) selectServices(services []*Service, configNamespace string, hostsByNamespace map[string]hostClassification) []*Service {
	importedServices := make([]*Service, 0)
	wildcardHosts, wnsFound := hostsByNamespace[wildcardNamespace]
	for _, s := range services {
		configNamespace := s.Attributes.Namespace

		// Check if there is an explicit import of form ns/* or ns/host
		if importedHosts, nsFound := hostsByNamespace[configNamespace]; nsFound {
			if svc := matchingAliasService(importedHosts, matchingService(importedHosts, s, ilw)); svc != nil {
				importedServices = append(importedServices, svc)
				continue
			}
		}

		// Check if there is an import of form */host or */*
		if wnsFound {
			if svc := matchingAliasService(wildcardHosts, matchingService(wildcardHosts, s, ilw)); svc != nil {
				importedServices = append(importedServices, svc)
			}
		}
	}

	if features.UnifiedSidecarScoping {
		type namespaceProvider struct {
			Namespace         string
			KubernetesService bool
		}
		validServices := make(map[host.Name]namespaceProvider, len(importedServices))
		for _, svc := range importedServices {
			np := namespaceProvider{Namespace: svc.Attributes.Namespace, KubernetesService: svc.Attributes.ServiceRegistry == provider.Kubernetes}
			ex, f := validServices[svc.Hostname]
			// Select a single namespace for a given hostname.
			// If the same hostname is imported from multiple namespaces, pick the one in the configNamespace
			// If neither are in configNamespace, an arbitrary one will be chosen, preferring Kubernetes services over ServiceEntry
			if !f {
				// First time we saw the hostname, take it
				validServices[svc.Hostname] = np
			} else if svc.Attributes.Namespace == configNamespace {
				// We have seen the hostname, but now its in the configNamespace which has precedence
				validServices[svc.Hostname] = np
			} else if !ex.KubernetesService && np.KubernetesService && (np.Namespace == configNamespace || ex.Namespace != configNamespace) {
				// We saw the hostname, but this time it is a Kubernetes service, rather than a ServiceEntry for the same hostname.
				// Make sure this isn't preempting the "same namespace wins" rule as well.
				// Prefer this one.
				validServices[svc.Hostname] = np
			}
		}

		// Filter down to just instances in scope for the service
		return slices.FilterInPlace(importedServices, func(svc *Service) bool {
			return validServices[svc.Hostname].Namespace == svc.Attributes.Namespace
		})
	}
	// Legacy path
	validServices := make(map[host.Name]string, len(importedServices))
	for _, svc := range importedServices {
		_, f := validServices[svc.Hostname]
		// Select a single namespace for a given hostname.
		// If the same hostname is imported from multiple namespaces, pick the one in the configNamespace
		// If neither are in configNamespace, an arbitrary one will be chosen
		if !f || svc.Attributes.Namespace == configNamespace {
			validServices[svc.Hostname] = svc.Attributes.Namespace
		}
	}

	// Filter down to just instances in scope for the service
	return slices.FilterInPlace(importedServices, func(svc *Service) bool {
		return validServices[svc.Hostname] == svc.Attributes.Namespace
	})
}

// Return the original service or a trimmed service which has a subset of the ports in original service.
func matchingService(importedHosts hostClassification, service *Service, ilw *IstioEgressListenerWrapper) *Service {
	if importedHosts.Matches(service.Hostname) {
		if ilw.matchPort {
			return serviceMatchingListenerPort(service, ilw)
		}
		return service
	}
	return nil
}

// matchingAliasService the original service or a trimmed service which has a subset of aliases, based on imports from sidecar
func matchingAliasService(importedHosts hostClassification, service *Service) *Service {
	if service == nil {
		return nil
	}
	matched := make([]NamespacedHostname, 0, len(service.Attributes.Aliases))
	for _, alias := range service.Attributes.Aliases {
		if importedHosts.Matches(alias.Hostname) {
			matched = append(matched, alias)
		}
	}

	if len(matched) == len(service.Attributes.Aliases) {
		return service
	}
	service = service.DeepCopy()
	service.Attributes.Aliases = matched
	return service
}

// serviceMatchingListenerPort constructs service with listener port.
func serviceMatchingListenerPort(service *Service, ilw *IstioEgressListenerWrapper) *Service {
	for _, port := range service.Ports {
		if port.Port == int(ilw.IstioListener.Port.GetNumber()) {
			// when there is one port, the port is listener port, we do not need to update the `service`,
			// so DeepCopy is not needed. Most services may only have one port, this special treatment
			// allows us to save DeepCopy consumption.
			if len(service.Ports) == 1 {
				return service
			}
			// when there are multiple ports, construct service with listener port
			sc := service.DeepCopy()
			sc.Ports = []*Port{port}
			return sc
		}
	}
	return nil
}

func serviceMatchingVirtualServicePorts(service *Service, vsDestPorts sets.Set[int]) *Service {
	// A value of 0 in vsDestPorts is used as a sentinel to indicate a dependency
	// on every port of the service.
	if len(vsDestPorts) == 0 || vsDestPorts.Contains(0) {
		return service
	}

	foundPorts := make([]*Port, 0)
	for _, port := range service.Ports {
		if vsDestPorts.Contains(port.Port) {
			foundPorts = append(foundPorts, port)
		}
	}

	if len(foundPorts) == len(service.Ports) {
		return service
	}

	if len(foundPorts) > 0 {
		sc := service.DeepCopy()
		sc.Ports = foundPorts
		return sc
	}

	// If the service has more than one port, and the Virtual Service only
	// specifies destination ports not found in the service, we'll simply
	// not add the service to the sidecar as an optimization, because
	// traffic will not route properly anyway. This matches the above
	// behavior in serviceMatchingListenerPort for ports specified on the
	// sidecar egress listener.
	log.Warnf("Failed to find any VirtualService destination ports %v exposed by Service %s", vsDestPorts, service.Hostname)
	return nil
}

// computeWildcardHostVirtualServiceIndex computes the wildcardHostVirtualServiceIndex for a given
// (sorted) list of virtualServices. This is used to optimize the lookup of the most specific wildcard host.
//
// N.B the caller MUST presort virtualServices based on the desired precedence for duplicate hostnames.
// This function will persist that order and not overwrite any previous entries for a given hostname.
func computeWildcardHostVirtualServiceIndex(virtualServices []config.Config, services []*Service) map[host.Name]types.NamespacedName {
	fqdnVirtualServiceHostIndex := make(map[host.Name]config.Config, len(virtualServices))
	wildcardVirtualServiceHostIndex := make(map[host.Name]config.Config, len(virtualServices))
	for _, vs := range virtualServices {
		v := vs.Spec.(*networking.VirtualService)
		for _, h := range v.Hosts {
			// We may have duplicate (not just overlapping) hosts; assume the list of VS is sorted already
			// and never overwrite existing entries
			if host.Name(h).IsWildCarded() {
				_, exists := wildcardVirtualServiceHostIndex[host.Name(h)]
				if !exists {
					wildcardVirtualServiceHostIndex[host.Name(h)] = vs
				}
			} else {
				_, exists := fqdnVirtualServiceHostIndex[host.Name(h)]
				if !exists {
					fqdnVirtualServiceHostIndex[host.Name(h)] = vs
				}
			}
		}
	}

	mostSpecificWildcardVsIndex := make(map[host.Name]types.NamespacedName)
	for _, svc := range services {
		_, ref, exists := MostSpecificHostMatch(svc.Hostname, fqdnVirtualServiceHostIndex, wildcardVirtualServiceHostIndex)
		if !exists {
			// This svc doesn't have a virtualService; skip
			continue
		}
		mostSpecificWildcardVsIndex[svc.Hostname] = ref.NamespacedName()
	}

	return mostSpecificWildcardVsIndex
}

func needsPortMatch(l *networking.IstioEgressListener) bool {
	// If a listener is defined with a port, we should match services with port except in the following case.
	//  - If Port's protocol is proxy protocol(HTTP_PROXY) in which case the egress listener is used as generic egress http proxy.
	return l != nil && l.Port.GetNumber() != 0 &&
		protocol.Parse(l.Port.Protocol) != protocol.HTTP_PROXY
}

type sidecarServiceIndex struct {
	svc   *Service
	index int // index record the position of the svc in slice
}

// append services to the sidecar scope, and merge services with the same hostname.
func (sc *SidecarScope) appendSidecarServices(servicesAdded map[host.Name]sidecarServiceIndex, s *Service) {
	if s == nil {
		return
	}
	if foundSvc, found := servicesAdded[s.Hostname]; !found {
		sc.services = append(sc.services, s)
		servicesAdded[s.Hostname] = sidecarServiceIndex{s, len(sc.services) - 1}
		sc.servicesByHostname[s.Hostname] = s
	} else {
		existing := foundSvc.svc
		// Skip services which are already added, this can happen when a service is explicitly requested
		// but is also a virtualservice destination, or if the service is a destination of multiple virtualservices
		if s == existing {
			log.Debugf("Service %s/%s from registry %s ignored as it is already added", s.Attributes.Namespace, s.Hostname, s.Attributes.ServiceRegistry)
			return
		}
		// We do not merge k8s service with any other services from other registries
		if existing.Attributes.ServiceRegistry == provider.Kubernetes && s.Attributes.ServiceRegistry != provider.Kubernetes {
			log.Debugf("Service %s/%s from registry %s ignored as there is an existing service in Kubernetes already %s/%s/%s",
				s.Attributes.Namespace, s.Hostname, s.Attributes.ServiceRegistry,
				existing.Attributes.Namespace, existing.Hostname, existing.Attributes.ServiceRegistry)
			return
		}
		// In some scenarios, there may be multiple Services defined for the same hostname due to ServiceEntry allowing
		// arbitrary hostnames. In these cases, we want to pick the first Service, which is the oldest. This ensures
		// newly created Services cannot take ownership unexpectedly. However, the Service is from Kubernetes it should
		// take precedence over ones not. This prevents someone from "domain squatting" on the hostname before a Kubernetes Service is created.
		if existing.Attributes.ServiceRegistry != provider.Kubernetes && s.Attributes.ServiceRegistry == provider.Kubernetes {
			log.Debugf("Service %s/%s from registry %s ignored as there is a Kubernetes service with the same host name %s/%s/%s",
				existing.Attributes.Namespace, existing.Hostname, existing.Attributes.ServiceRegistry,
				s.Attributes.Namespace, s.Hostname, s.Attributes.ServiceRegistry)
			// replace service in slice
			sc.services[foundSvc.index] = s
			// Update index as well, so that future reads will merge into the new service
			foundSvc.svc = s
			servicesAdded[foundSvc.svc.Hostname] = foundSvc
			sc.servicesByHostname[s.Hostname] = s
			return
		}

		// if both services have the same ports, no need to merge and we save a deepcopy
		if existing.Ports.Equals(s.Ports) {
			log.Debugf("Service %s/%s from registry %s ignored as it has the same ports as %s/%s/%s", s.Attributes.Namespace, s.Hostname,
				s.Attributes.ServiceRegistry, existing.Attributes.Namespace, existing.Hostname, existing.Attributes.ServiceRegistry)
			return
		}

		if !canMergeServices(existing, s) {
			log.Debugf("Service %s/%s from registry %s ignored by %s/%s/%s", s.Attributes.Namespace, s.Hostname, s.Attributes.ServiceRegistry,
				existing.Attributes.Namespace, existing.Hostname, existing.Attributes.ServiceRegistry)
			return
		}

		// If it comes here, it means we can merge the services.
		// Merge the ports to service when each listener generates partial service.
		// We only merge if the found service is in the same namespace as the one we're trying to add
		copied := foundSvc.svc.DeepCopy()
		for _, p := range s.Ports {
			found := false
			for _, osp := range copied.Ports {
				if p.Port == osp.Port {
					found = true
					break
				}
			}
			if !found {
				copied.Ports = append(copied.Ports, p)
			}
		}
		// replace service in slice
		sc.services[foundSvc.index] = copied
		// Update index as well, so that future reads will merge into the new service
		foundSvc.svc = copied
		servicesAdded[foundSvc.svc.Hostname] = foundSvc
		// update the existing service in the map to the merged one
		sc.servicesByHostname[s.Hostname] = copied
	}
}

func canMergeServices(s1, s2 *Service) bool {
	// Hostname has been compared in the caller `appendSidecarServices`, so we donot need to compare again.
	if s1.Attributes.Namespace != s2.Attributes.Namespace {
		return false
	}
	if s1.Resolution != s2.Resolution {
		return false
	}
	// kuberneres service registry has been checked before
	if s1.Attributes.ServiceRegistry != s2.Attributes.ServiceRegistry {
		return false
	}

	if !maps.Equal(s1.Attributes.Labels, s2.Attributes.Labels) {
		return false
	}

	if !maps.Equal(s1.Attributes.LabelSelectors, s2.Attributes.LabelSelectors) {
		return false
	}

	if !maps.Equal(s1.Attributes.ExportTo, s2.Attributes.ExportTo) {
		return false
	}

	return true
}

// Pick the Service namespace visible to the configNamespace namespace.
// If it does not exist, return an empty string,
// If there are more than one, pick the first alphabetically.
func pickFirstVisibleNamespace(ps *PushContext, byNamespace map[string]*Service, configNamespace string) string {
	nss := make([]string, 0, len(byNamespace))
	for ns := range byNamespace {
		if ps.IsServiceVisible(byNamespace[ns], configNamespace) {
			nss = append(nss, ns)
		}
	}
	if len(nss) > 0 {
		sort.Strings(nss)
		return nss[0]
	}
	return ""
}
