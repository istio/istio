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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

const (
	wildcardNamespace = "*"
	currentNamespace  = "."
	wildcardService   = host.Name("*")
)

var (
	sidecarScopeKnownConfigTypes = map[config.GroupVersionKind]struct{}{
		gvk.ServiceEntry:    {},
		gvk.VirtualService:  {},
		gvk.DestinationRule: {},
		gvk.Sidecar:         {},
	}

	// clusterScopedConfigTypes includes configs when they are in root namespace,
	// they will be applied to all namespaces within the cluster.
	clusterScopedConfigTypes = map[config.GroupVersionKind]struct{}{
		gvk.EnvoyFilter:           {},
		gvk.AuthorizationPolicy:   {},
		gvk.RequestAuthentication: {},
	}
)

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
	// The crd itself. Can be nil if we are constructing the default
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
	destinationRules map[host.Name][]*consolidatedDestRule

	// OutboundTrafficPolicy defines the outbound traffic policy for this sidecar.
	// If OutboundTrafficPolicy is ALLOW_ANY traffic to unknown destinations will
	// be forwarded.
	OutboundTrafficPolicy *networking.OutboundTrafficPolicy

	// Set of known configs this sidecar depends on.
	// This field will be used to determine the config/resource scope
	// which means which config changes will affect the proxies within this scope.
	configDependencies map[uint64]struct{}

	// The namespace to treat as the administrative root namespace for
	// Istio configuration.
	//
	// Changes to Sidecar resources in this namespace will trigger a push.
	RootNamespace string
}

// MarshalJSON implements json.Marshaller
func (sc *SidecarScope) MarshalJSON() ([]byte, error) {
	// Json cannot expose unexported fields, so copy the ones we want here
	return json.MarshalIndent(map[string]interface{}{
		"version":               sc.Version,
		"rootNamespace":         sc.RootNamespace,
		"name":                  sc.Name,
		"namespace":             sc.Namespace,
		"outboundTrafficPolicy": sc.OutboundTrafficPolicy,
		"services":              sc.services,
		"sidecar":               sc.Sidecar,
		"destinationRules":      sc.destinationRules,
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

	listenerHosts map[string][]host.Name
}

const defaultSidecar = "default-sidecar"

// DefaultSidecarScopeForNamespace is a sidecar scope object with a default catch all egress listener
// that matches the default Istio behavior: a sidecar has listeners for all services in the mesh
// We use this scope when the user has not set any sidecar Config for a given config namespace.
func DefaultSidecarScopeForNamespace(ps *PushContext, configNamespace string) *SidecarScope {
	defaultEgressListener := &IstioEgressListenerWrapper{
		IstioListener: &networking.IstioEgressListener{
			Hosts: []string{"*/*"},
		},
	}
	defaultEgressListener.services = ps.servicesExportedToNamespace(configNamespace)
	defaultEgressListener.virtualServices = ps.VirtualServicesForGateway(configNamespace, constants.IstioMeshGateway)

	out := &SidecarScope{
		Name:               defaultSidecar,
		Namespace:          configNamespace,
		EgressListeners:    []*IstioEgressListenerWrapper{defaultEgressListener},
		services:           defaultEgressListener.services,
		destinationRules:   make(map[host.Name][]*consolidatedDestRule),
		servicesByHostname: make(map[host.Name]*Service, len(defaultEgressListener.services)),
		configDependencies: make(map[uint64]struct{}),
		RootNamespace:      ps.Mesh.RootNamespace,
		Version:            ps.PushVersion,
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	for _, s := range out.services {
		// In some scenarios, there may be multiple Services defined for the same hostname due to ServiceEntry allowing
		// arbitrary hostnames. In these cases, we want to pick the first Service, which is the oldest. This ensures
		// newly created Services cannot take ownership unexpectedly.
		// However, the Service is from Kubernetes it should take precedence over ones not. This prevents someone from
		// "domain squatting" on the hostname before a Kubernetes Service is created.
		// This relies on the assumption that
		if existing, f := out.servicesByHostname[s.Hostname]; f &&
			!(existing.Attributes.ServiceRegistry != provider.Kubernetes && s.Attributes.ServiceRegistry == provider.Kubernetes) {
			continue
		}
		out.servicesByHostname[s.Hostname] = s
		if dr := ps.destinationRule(configNamespace, s); dr != nil {
			out.destinationRules[s.Hostname] = dr
		}
		out.AddConfigDependencies(ConfigKey{
			Kind:      gvk.ServiceEntry,
			Name:      string(s.Hostname),
			Namespace: s.Attributes.Namespace,
		})
	}

	for _, drList := range out.destinationRules {
		for _, dr := range drList {
			for _, namespacedName := range dr.from {
				out.AddConfigDependencies(ConfigKey{
					Kind:      gvk.DestinationRule,
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				})
			}
		}
	}

	for _, el := range out.EgressListeners {
		// add dependencies on delegate virtual services
		delegates := ps.DelegateVirtualServicesConfigKey(el.virtualServices)
		for _, delegate := range delegates {
			out.AddConfigDependencies(delegate)
		}
		for _, vs := range el.virtualServices {
			out.AddConfigDependencies(ConfigKey{
				Kind:      gvk.VirtualService,
				Name:      vs.Name,
				Namespace: vs.Namespace,
			})
		}
	}

	if ps.Mesh.OutboundTrafficPolicy != nil {
		out.OutboundTrafficPolicy = &networking.OutboundTrafficPolicy{
			Mode: networking.OutboundTrafficPolicy_Mode(ps.Mesh.OutboundTrafficPolicy.Mode),
		}
	}

	return out
}

// ConvertToSidecarScope converts from Sidecar config to SidecarScope object
func ConvertToSidecarScope(ps *PushContext, sidecarConfig *config.Config, configNamespace string) *SidecarScope {
	if sidecarConfig == nil {
		return DefaultSidecarScopeForNamespace(ps, configNamespace)
	}

	sidecar := sidecarConfig.Spec.(*networking.Sidecar)
	out := &SidecarScope{
		Name:               sidecarConfig.Name,
		Namespace:          configNamespace,
		Sidecar:            sidecar,
		configDependencies: make(map[uint64]struct{}),
		RootNamespace:      ps.Mesh.RootNamespace,
		Version:            ps.PushVersion,
	}

	out.AddConfigDependencies(ConfigKey{
		Kind:      gvk.Sidecar,
		Name:      sidecarConfig.Name,
		Namespace: sidecarConfig.Namespace,
	})

	egressConfigs := sidecar.Egress
	// If egress not set, setup a default listener
	if len(egressConfigs) == 0 {
		egressConfigs = append(egressConfigs, &networking.IstioEgressListener{Hosts: []string{"*/*"}})
	}
	out.EgressListeners = make([]*IstioEgressListenerWrapper, 0, len(egressConfigs))
	for _, e := range egressConfigs {
		out.EgressListeners = append(out.EgressListeners,
			convertIstioListenerToWrapper(ps, configNamespace, e))
	}

	// Now collect all the imported services across all egress listeners in
	// this sidecar crd. This is needed to generate CDS output
	out.services = make([]*Service, 0)
	type serviceIndex struct {
		svc   *Service
		index int // index record the position of the svc in slice
	}
	servicesAdded := make(map[host.Name]serviceIndex)
	addService := func(s *Service) {
		if s == nil {
			return
		}
		if foundSvc, found := servicesAdded[s.Hostname]; !found {
			out.AddConfigDependencies(ConfigKey{
				Kind:      gvk.ServiceEntry,
				Name:      string(s.Hostname),
				Namespace: s.Attributes.Namespace,
			})
			out.services = append(out.services, s)
			servicesAdded[s.Hostname] = serviceIndex{s, len(out.services) - 1}
		} else if foundSvc.svc.Attributes.Namespace == s.Attributes.Namespace && s.Ports != nil && len(s.Ports) > 0 {
			// merge the ports to service when each listener generates partial service
			// we only merge if the found service is in the same namespace as the one we're trying to add
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
			out.services[foundSvc.index] = copied
			// Update index as well, so that future reads will merge into the new service
			foundSvc.svc = copied
			servicesAdded[foundSvc.svc.Hostname] = foundSvc
		}
	}

	for _, listener := range out.EgressListeners {
		// First add the explicitly requested services, which take priority
		for _, s := range listener.services {
			addService(s)
		}
		// add dependencies on delegate virtual services
		delegates := ps.DelegateVirtualServicesConfigKey(listener.virtualServices)
		for _, delegate := range delegates {
			out.AddConfigDependencies(delegate)
		}

		matchPort := needsPortMatch(listener)
		// Infer more possible destinations from virtual services
		// Services chosen here will not override services explicitly requested in listener.services.
		// That way, if there is ambiguity around what hostname to pick, a user can specify the one they
		// want in the hosts field, and the potentially random choice below won't matter
		for _, vs := range listener.virtualServices {
			v := vs.Spec.(*networking.VirtualService)
			out.AddConfigDependencies(ConfigKey{
				Kind:      gvk.VirtualService,
				Name:      vs.Name,
				Namespace: vs.Namespace,
			})

			for h, ports := range virtualServiceDestinations(v) {
				// Default to this hostname in our config namespace
				if s, ok := ps.ServiceIndex.HostnameAndNamespace[host.Name(h)][configNamespace]; ok {
					// This won't overwrite hostnames that have already been found eg because they were requested in hosts
					var vss *Service
					if matchPort {
						vss = serviceMatchingListenerPort(s, listener)
					} else {
						vss = serviceMatchingVirtualServicePorts(s, ports)
					}
					if vss != nil {
						addService(vss)
					}
				} else {

					// We couldn't find the hostname in our config namespace
					// We have to pick one arbitrarily for now, so we'll pick the first namespace alphabetically
					// TODO: could we choose services more intelligently based on their ports?
					byNamespace := ps.ServiceIndex.HostnameAndNamespace[host.Name(h)]
					if len(byNamespace) == 0 {
						// This hostname isn't found anywhere
						log.Debugf("Could not find service hostname %s parsed from %s", h, vs.Key())
						continue
					}

					ns := make([]string, 0, len(byNamespace))
					for k := range byNamespace {
						if ps.IsServiceVisible(byNamespace[k], configNamespace) {
							ns = append(ns, k)
						}
					}
					if len(ns) > 0 {
						sort.Strings(ns)
						// Pick first namespace alphabetically
						// This won't overwrite hostnames that have already been found eg because they were requested in hosts
						var vss *Service
						if matchPort {
							vss = serviceMatchingListenerPort(byNamespace[ns[0]], listener)
						} else {
							vss = serviceMatchingVirtualServicePorts(byNamespace[ns[0]], ports)
						}
						if vss != nil {
							addService(vss)
						}
					}
				}
			}
		}
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	out.servicesByHostname = make(map[host.Name]*Service, len(out.services))
	out.destinationRules = make(map[host.Name][]*consolidatedDestRule)
	for _, s := range out.services {
		out.servicesByHostname[s.Hostname] = s
		drList := ps.destinationRule(configNamespace, s)
		if drList != nil {
			out.destinationRules[s.Hostname] = drList
			for _, dr := range drList {
				for _, key := range dr.from {
					out.AddConfigDependencies(ConfigKey{
						Kind:      gvk.DestinationRule,
						Name:      key.Name,
						Namespace: key.Namespace,
					})
				}
			}
		}
	}

	if sidecar.OutboundTrafficPolicy == nil {
		if ps.Mesh.OutboundTrafficPolicy != nil {
			out.OutboundTrafficPolicy = &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_Mode(ps.Mesh.OutboundTrafficPolicy.Mode),
			}
		}
	} else {
		out.OutboundTrafficPolicy = sidecar.OutboundTrafficPolicy
	}

	return out
}

func convertIstioListenerToWrapper(ps *PushContext, configNamespace string,
	istioListener *networking.IstioEgressListener,
) *IstioEgressListenerWrapper {
	out := &IstioEgressListenerWrapper{
		IstioListener: istioListener,
	}

	out.listenerHosts = make(map[string][]host.Name)
	for _, h := range istioListener.Hosts {
		parts := strings.SplitN(h, "/", 2)
		if len(parts) < 2 {
			log.Errorf("Illegal host in sidecar resource: %s, host must be of form namespace/dnsName", h)
			continue
		}
		if parts[0] == currentNamespace {
			parts[0] = configNamespace
		}
		if _, exists := out.listenerHosts[parts[0]]; !exists {
			out.listenerHosts[parts[0]] = make([]host.Name, 0)
		}
		out.listenerHosts[parts[0]] = append(out.listenerHosts[parts[0]], host.Name(parts[1]))
	}

	vses := ps.VirtualServicesForGateway(configNamespace, constants.IstioMeshGateway)
	out.virtualServices = SelectVirtualServices(vses, out.listenerHosts)
	svces := ps.servicesExportedToNamespace(configNamespace)
	out.services = out.selectServices(svces, configNamespace, out.listenerHosts)

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

// Services returns the list of services imported by this egress listener
func (ilw *IstioEgressListenerWrapper) Services() []*Service {
	return ilw.services
}

// VirtualServices returns the list of virtual services imported by this
// egress listener
func (ilw *IstioEgressListenerWrapper) VirtualServices() []config.Config {
	return ilw.virtualServices
}

// DependsOnConfig determines if the proxy depends on the given config.
// Returns whether depends on this config or this kind of config is not scoped(unknown to be depended) here.
func (sc *SidecarScope) DependsOnConfig(config ConfigKey) bool {
	if sc == nil {
		return true
	}

	// This kind of config will trigger a change if made in the root namespace or the same namespace
	if _, f := clusterScopedConfigTypes[config.Kind]; f {
		return config.Namespace == sc.RootNamespace || config.Namespace == sc.Namespace
	}

	// This kind of config is unknown to sidecarScope.
	if _, f := sidecarScopeKnownConfigTypes[config.Kind]; !f {
		return true
	}

	_, exists := sc.configDependencies[config.HashCode()]
	return exists
}

// AddConfigDependencies add extra config dependencies to this scope. This action should be done before the
// SidecarScope being used to avoid concurrent read/write.
func (sc *SidecarScope) AddConfigDependencies(dependencies ...ConfigKey) {
	if sc == nil {
		return
	}
	if sc.configDependencies == nil {
		sc.configDependencies = make(map[uint64]struct{})
	}

	for _, config := range dependencies {
		sc.configDependencies[config.HashCode()] = struct{}{}
	}
}

// DestinationRule returns a destinationrule for a svc.
func (sc *SidecarScope) DestinationRule(direction TrafficDirection, proxy *Proxy, svc host.Name) *config.Config {
	destinationRules := sc.destinationRules[svc]
	var catchAllDr *config.Config
	for _, destRule := range destinationRules {
		destinationRule := destRule.rule.Spec.(*networking.DestinationRule)
		if destinationRule.GetWorkloadSelector() == nil {
			catchAllDr = destRule.rule
		}
		// filter DestinationRule based on workloadSelector for outbound configs.
		// WorkloadSelector configuration is honored only for outbound configuration, because
		// for inbound configuration, the settings at sidecar would be more explicit and the preferred way forward.
		if sc.Namespace == destRule.rule.Namespace &&
			destinationRule.GetWorkloadSelector() != nil && direction == TrafficDirectionOutbound {
			workloadSelector := labels.Instance(destinationRule.GetWorkloadSelector().GetMatchLabels())
			// return destination rule if workload selector matches
			if workloadSelector.SubsetOf(proxy.Metadata.Labels) {
				return destRule.rule
			}
		}
	}
	// If there is no workload specific destinationRule, return the wild carded dr if present.
	if catchAllDr != nil {
		return catchAllDr
	}
	return nil
}

// Services returns the list of services that are visible to a sidecar.
func (sc *SidecarScope) Services() []*Service {
	return sc.services
}

// Return filtered services through the hosts field in the egress portion of the Sidecar config.
// Note that the returned service could be trimmed.
func (ilw *IstioEgressListenerWrapper) selectServices(services []*Service, configNamespace string, hosts map[string][]host.Name) []*Service {
	importedServices := make([]*Service, 0)
	wildcardHosts, wnsFound := hosts[wildcardNamespace]
	for _, s := range services {
		configNamespace := s.Attributes.Namespace

		// Check if there is an explicit import of form ns/* or ns/host
		if importedHosts, nsFound := hosts[configNamespace]; nsFound {
			if svc := matchingService(importedHosts, s, ilw); svc != nil {
				importedServices = append(importedServices, svc)
				continue
			}
		}
		if wnsFound { // Check if there is an import of form */host or */*
			if svc := matchingService(wildcardHosts, s, ilw); svc != nil {
				importedServices = append(importedServices, svc)
			}
		}
	}

	validServices := make(map[host.Name]string)
	for _, svc := range importedServices {
		_, f := validServices[svc.Hostname]
		// Select a single namespace for a given hostname.
		// If the same hostname is imported from multiple namespaces, pick the one in the configNamespace
		// If neither are in configNamespace, an arbitrary one will be chosen
		if !f || svc.Attributes.Namespace == configNamespace {
			validServices[svc.Hostname] = svc.Attributes.Namespace
		}
	}

	filteredServices := make([]*Service, 0)
	// Filter down to just instances in scope for the service
	for _, svc := range importedServices {
		if validServices[svc.Hostname] == svc.Attributes.Namespace {
			filteredServices = append(filteredServices, svc)
		}
	}
	return filteredServices
}

// Return the original service or a trimmed service which has a subset of the ports in original service.
func matchingService(importedHosts []host.Name, service *Service, ilw *IstioEgressListenerWrapper) *Service {
	matchPort := needsPortMatch(ilw)
	for _, importedHost := range importedHosts {
		// Check if the hostnames match per usual hostname matching rules
		if service.Hostname.SubsetOf(importedHost) {
			if matchPort {
				return serviceMatchingListenerPort(service, ilw)
			}
			return service
		}
	}
	return nil
}

// serviceMatchingListenerPort constructs service with listener port.
func serviceMatchingListenerPort(service *Service, ilw *IstioEgressListenerWrapper) *Service {
	for _, port := range service.Ports {
		if port.Port == int(ilw.IstioListener.Port.GetNumber()) {
			sc := service.DeepCopy()
			sc.Ports = []*Port{port}
			return sc
		}
	}
	return nil
}

func serviceMatchingVirtualServicePorts(service *Service, vsDestPorts sets.IntSet) *Service {
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

func needsPortMatch(ilw *IstioEgressListenerWrapper) bool {
	// If a listener is defined with a port, we should match services with port except in the following case.
	//  - If Port's protocol is proxy protocol(HTTP_PROXY) in which case the egress listener is used as generic egress http proxy.
	return ilw.IstioListener != nil && ilw.IstioListener.Port.GetNumber() != 0 &&
		protocol.Parse(ilw.IstioListener.Port.Protocol) != protocol.HTTP_PROXY
}
