// Copyright 2019 Istio Authors
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
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	networking "istio.io/api/networking/v1alpha3"
)

const (
	wildcardNamespace = "*"
	currentNamespace  = "."
	wildcardService   = Hostname("*")
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
	// The crd itself. Can be nil if we are constructing the default
	// sidecar scope
	Config *Config

	// Set of egress listeners, and their associated services.  A sidecar
	// scope should have either ingress/egress listeners or both.  For
	// every proxy workload that maps to a sidecar API object (or the
	// default object), we will go through every egress listener in the
	// object and process the Envoy listener or RDS based on the imported
	// services/virtual services in that listener.
	EgressListeners []*IstioEgressListenerWrapper

	// HasCustomIngressListeners is a convenience variable that if set to
	// true indicates that the config object has one or more listeners.
	// If set to false, networking code should derive the inbound
	// listeners from the proxy service instances
	HasCustomIngressListeners bool

	// Union of services imported across all egress listeners for use by CDS code.
	// Right now, we include all the ports in these services.
	// TODO: Trim the ports in the services to only those referred to by the
	// egress listeners.
	services []*Service

	// Destination rules imported across all egress listeners. This
	// contains the computed set based on public/private destination rules
	// as well as the inherited ones, in addition to the wildcard matches
	// such as *.com applying to foo.bar.com. Each hostname in this map
	// corresponds to a service in the services array above. When computing
	// CDS, we simply have to find the matching service and return the
	// destination rule.
	destinationRules map[Hostname]*Config

	// CDSOutboundClusters is the CDS output for sidecars that map to this
	// sidecarScope object. Contains the outbound clusters only, indexed
	// by localities
	CDSOutboundClusters map[string][]*xdsapi.Cluster
}

// IstioEgressListenerWrapper is a wrapper for
// networking.IstioEgressListener object. The wrapper provides performance
// optimizations as it allows us to precompute and store the list of
// services/virtualServices that apply to this listener.
type IstioEgressListenerWrapper struct {
	// The actual IstioEgressListener api object from the Config. It can be
	// nil if this is for the default sidecar scope.
	IstioListener *networking.IstioEgressListener

	// A preprocessed form of networking.IstioEgressListener.hosts field.
	// The hosts field has entries of the form namespace/dnsName. For
	// example ns1/*, */*, */foo.tcp.com, etc. This map preprocesses all
	// these string fields into a map of namespace and services.
	// We cannot use a map of Hostnames because Hostname match allows
	// wildcard matching semantics (i.e. foo.bar.com will match import like *.com).
	// Go's map/hash data structure doesn't do such semantic matches
	listenerHosts map[string][]Hostname

	// List of services imported by this egress listener extracted from the
	// listenerHosts above. This will be used by LDS and RDS code when
	// building the set of virtual hosts or the tcp filterchain matches for
	// a given listener port. Two listeners, on user specified ports or
	// unix domain sockets could have completely different sets of
	// services. So a global list of services per sidecar scope will be
	// incorrect. Hence the per listener set of services.
	services []*Service

	// List of virtual services imported by this egress listener extracted
	// from the listenerHosts above. As with per listener services, this
	// will be used by RDS code to compute the virtual host configs for
	// http listeners, as well as by TCP/TLS filter code to compute the
	// service routing configs and the filter chain matches. We need a
	// virtualService set per listener and not one per sidecarScope because
	// each listener imports an independent set of virtual services.
	// Listener 1 could import a public virtual service for serviceA from
	// namespace A that has some path rewrite, while listener2 could import
	// a private virtual service for serviceA from the local namespace,
	// with a different path rewrite or no path rewrites.
	virtualServices []Config
}

// DefaultSidecarScope is a sidecar scope object with a default catch all egress listener
// that matches the default Istio behavior: a sidecar has listeners for all services in the mesh
// We use this scope when the user has not set any sidecar Config for a given config namespace.
func DefaultSidecarScopeForNamespace(ps *PushContext, configNamespace string) *SidecarScope {
	dummyNode := Proxy{
		ConfigNamespace: configNamespace,
	}

	defaultEgressListener := &IstioEgressListenerWrapper{
		listenerHosts: map[string][]Hostname{wildcardNamespace: {wildcardService}},
	}
	defaultEgressListener.services = ps.Services(&dummyNode)

	meshGateway := map[string]bool{IstioMeshGateway: true}
	defaultEgressListener.virtualServices = ps.VirtualServices(&dummyNode, meshGateway)

	out := &SidecarScope{
		EgressListeners:  []*IstioEgressListenerWrapper{defaultEgressListener},
		services:         defaultEgressListener.services,
		destinationRules: make(map[Hostname]*Config),
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	for _, s := range out.services {
		out.destinationRules[s.Hostname] = ps.DestinationRule(&dummyNode, s)
	}

	return out
}

// ConvertToSidecarScope converts from Sidecar config to SidecarScope object
func ConvertToSidecarScope(ps *PushContext, sidecarConfig *Config, configNamespace string) *SidecarScope {
	if sidecarConfig == nil {
		return DefaultSidecarScopeForNamespace(ps, configNamespace)
	}

	r := sidecarConfig.Spec.(*networking.Sidecar)
	out := &SidecarScope{}

	out.EgressListeners = make([]*IstioEgressListenerWrapper, 0)
	for _, e := range r.Egress {
		out.EgressListeners = append(out.EgressListeners,
			convertIstioListenerToWrapper(ps, configNamespace, e))
	}

	// Now collect all the imported services across all egress listeners in
	// this sidecar crd. This is needed to generate CDS output
	out.services = make([]*Service, 0)
	servicesAdded := make(map[string]struct{})
	dummyNode := Proxy{
		ConfigNamespace: configNamespace,
	}

	for _, listener := range out.EgressListeners {
		for _, s := range listener.services {
			// TODO: port merging when each listener generates a partial service
			if _, found := servicesAdded[string(s.Hostname)]; !found {
				servicesAdded[string(s.Hostname)] = struct{}{}
				out.services = append(out.services, s)
			}
		}
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules
	// that these services need
	out.destinationRules = make(map[Hostname]*Config)
	for _, s := range out.services {
		out.destinationRules[s.Hostname] = ps.DestinationRule(&dummyNode, s)
	}

	out.Config = sidecarConfig
	if len(r.Ingress) > 0 {
		out.HasCustomIngressListeners = true
	}

	return out
}

func convertIstioListenerToWrapper(ps *PushContext, configNamespace string,
	istioListener *networking.IstioEgressListener) *IstioEgressListenerWrapper {

	out := &IstioEgressListenerWrapper{
		IstioListener: istioListener,
		listenerHosts: make(map[string][]Hostname),
	}

	if istioListener.Hosts != nil {
		for _, h := range istioListener.Hosts {
			parts := strings.SplitN(h, "/", 2)
			if parts[0] == currentNamespace {
				parts[0] = configNamespace
			}
			if _, exists := out.listenerHosts[parts[0]]; !exists {
				out.listenerHosts[parts[0]] = make([]Hostname, 0)
			}

			out.listenerHosts[parts[0]] = append(out.listenerHosts[parts[0]], Hostname(parts[1]))
		}
	}

	dummyNode := Proxy{
		ConfigNamespace: configNamespace,
	}

	out.services = out.selectServices(ps.Services(&dummyNode))
	meshGateway := map[string]bool{IstioMeshGateway: true}
	out.virtualServices = out.selectVirtualServices(ps.VirtualServices(&dummyNode, meshGateway))

	return out
}

// Services returns the list of services imported across all egress listeners by this
// Sidecar config
func (sc *SidecarScope) Services() []*Service {
	if sc == nil {
		return nil
	}

	return sc.services
}

// DestinationRule returns the destination rule applicable for a given hostname
// used by CDS code
func (sc *SidecarScope) DestinationRule(hostname Hostname) *Config {
	if sc == nil {
		return nil
	}

	return sc.destinationRules[hostname]
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

// Services returns the list of services imported by this egress listener
func (ilw *IstioEgressListenerWrapper) Services() []*Service {
	if ilw == nil {
		return nil
	}

	return ilw.services
}

// VirtualServices returns the list of virtual services imported by this
// egress listener
func (ilw *IstioEgressListenerWrapper) VirtualServices() []Config {
	if ilw == nil {
		return nil
	}

	return ilw.virtualServices
}

// Given a list of virtual services visible to this namespace,
// selectVirtualServices returns the list of virtual services that are
// applicable to this egress listener, based on the hosts field specified
// in the API. This code is called only once during the construction of the
// listener wrapper. The parent object (sidecarScope) and its listeners are
// constructed only once and reused for every sidecar that selects this
// sidecarScope object. Selection is based on labels at the moment.
func (ilw *IstioEgressListenerWrapper) selectVirtualServices(virtualServices []Config) []Config {
	importedVirtualServices := make([]Config, 0)
	for _, c := range virtualServices {
		configNamespace := c.Namespace
		rule := c.Spec.(*networking.VirtualService)

		// Selection algorithm:
		// virtualservices have a list of hosts in the API spec
		// Sidecars have a list of hosts in the api spec (namespace/host format)
		// if any host in the virtualService.hosts matches the sidecar's egress'
		// entry <virtualServiceNamespace>/virtualServiceHost, select the virtual service
		// and break out of the loop.
		// OR if any host in the virtualService.hosts matches the sidecar's egress'
		// entry */virtualServiceHost, select the virtual service and break out of the loop.

		// Check if there is an explicit import of form ns/* or ns/host
		if importedHosts, nsFound := ilw.listenerHosts[configNamespace]; nsFound {
			for _, importedHost := range importedHosts {
				// Check if the hostnames match per usual hostname matching rules
				hostFound := false
				for _, h := range rule.Hosts {
					// TODO: This is a bug. VirtualServices can have many hosts
					// while the user might be importing only a single host
					// We need to generate a new VirtualService with just the matched host
					if importedHost.Matches(Hostname(h)) {
						importedVirtualServices = append(importedVirtualServices, c)
						hostFound = true
						break
					}
				}

				if hostFound {
					break
				}
			}
		}

		// Check if there is an import of form */host or */*
		if importedHosts, wnsFound := ilw.listenerHosts[wildcardNamespace]; wnsFound {
			for _, importedHost := range importedHosts {
				// Check if the hostnames match per usual hostname matching rules
				hostFound := false
				for _, h := range rule.Hosts {
					// TODO: This is a bug. VirtualServices can have many hosts
					// while the user might be importing only a single host
					// We need to generate a new VirtualService with just the matched host
					if importedHost.Matches(Hostname(h)) {
						importedVirtualServices = append(importedVirtualServices, c)
						hostFound = true
						break
					}
				}

				if hostFound {
					break
				}
			}
		}
	}

	return importedVirtualServices
}

// selectServices returns the list of services selected through the hosts field
// in the egress portion of the Sidecar config
func (ilw *IstioEgressListenerWrapper) selectServices(services []*Service) []*Service {

	importedServices := make([]*Service, 0)
	for _, s := range services {
		configNamespace := s.Attributes.Namespace
		// Check if there is an explicit import of form ns/* or ns/host
		if importedHosts, nsFound := ilw.listenerHosts[configNamespace]; nsFound {
			hostFound := false
			for _, importedHost := range importedHosts {
				// Check if the hostnames match per usual hostname matching rules
				if importedHost.Matches(s.Hostname) {
					// TODO: See if the service's ports match.
					//   If there is a listener port for this Listener, then
					//   check if the service has a port of same value.
					//   If not, check if the service has a single port - and choose that port
					//   if service has multiple ports none of which match the listener port, check if there is
					//   a virtualService with match Port
					importedServices = append(importedServices, s)
					hostFound = true
					break
				}
			}
			if hostFound {
				continue
			}
		}

		// Check if there is an import of form */host or */*
		if importedHosts, wnsFound := ilw.listenerHosts[wildcardNamespace]; wnsFound {
			for _, importedHost := range importedHosts {
				// Check if the hostnames match per usual hostname matching rules
				if importedHost.Matches(s.Hostname) {
					importedServices = append(importedServices, s)
					break
				}
			}
		}
	}

	return importedServices
}
