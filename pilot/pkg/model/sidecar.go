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

	networking "istio.io/api/networking/v1alpha3"
)

const (
	wildcardNamespace = "*"
	wildcardService   = Hostname("*")
)

// SidecarScope is a wrapper over the Sidecar resource with some
// preprocessed data to determine the list of services, virtualServices,
// and destinationRules that are accessible to a given sidecar.
type SidecarScope struct {
	// The crd itself. Can be nil if we are constructing the default
	// sidecar scope
	Config *Config

	// set of egress listeners, and their associated services.
	// A sidecar scope should have either ingress/egress listeners or both.
	EgressListeners []*IstioEgressListenerWrapper

	// // set of ingress listeners
	// // A sidecar scope should have either ingress/egress listeners or both.
	// IngressListeners []*IstioIngressListenerWrapper

	// Union of services imported across all egress listeners for use by CDS code.
	// Right now, we include all the ports in these services.
	// TODO: Trim the ports in the services to only those referred to by the
	// egress listeners.
	allImportedServices []*Service

	// Destination rules imported across all egress listeners
	allImportedDestinationRules map[Hostname]*Config

	// VirtualServices imported across all egress listeners
	allImportedVirtualServices []Config
}

// IstioEgressListenerWrapper is a wrapper for networking.IstioEgressListener object.
// It has the parsed form of the hosts field, encompassing a list of services
// per sidecar listener object.
type IstioEgressListenerWrapper struct {
	// The actual IstioEgressListener api object from the Config. It can be nil if
	// this is for the default sidecar scope.
	IstioListener *networking.IstioEgressListener

	// TODO: Unix domain socket

	// optional. The port on which this listener should be configured. If
	// omitted, we infer from services imported
	ListenerPort *Port

	// parsed form of the hosts field
	// map of namespace and services
	importMap map[string]Hostname

	// List of services imported by this egress listener
	// extracted from the importMap above.
	importedServices []*Service
}

// DefaultSidecarScope is a sidecar scope object with a default catch all egress listener
// that matches the default Istio behavior: a sidecar has listeners for all services in the mesh
// We use this scope when the user has not set any sidecar Config for a given config namespace.
func DefaultSidecarScopeForNamespace(ps *PushContext, configNamespace string) *SidecarScope {
	dummyNode := Proxy{
		ConfigNamespace: configNamespace,
	}

	defaultEgressListener := &IstioEgressListenerWrapper{
		importMap: map[string]Hostname{wildcardNamespace: wildcardService},
	}
	defaultEgressListener.importedServices = ps.Services(&dummyNode)

	out := &SidecarScope{
		EgressListeners:             []*IstioEgressListenerWrapper{defaultEgressListener},
		allImportedServices:         defaultEgressListener.importedServices,
		allImportedDestinationRules: make(map[Hostname]*Config),
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules and virtualServices
	// that these services need
	for _, s := range out.allImportedServices {
		out.allImportedDestinationRules[s.Hostname] = ps.DestinationRule(&dummyNode, s.Hostname)
	}

	meshGateway := map[string]bool{IstioMeshGateway: true}
	out.allImportedVirtualServices = ps.VirtualServices(&dummyNode, meshGateway)
	return out
}

// ConvertToSidecarScope converts from Sidecar config to SidecarScope object
func ConvertToSidecarScope(ps *PushContext, sidecarConfig *Config) *SidecarScope {

	out := &SidecarScope{
		Config:                      sidecarConfig,
		allImportedServices:         make([]*Service, 0),
		allImportedDestinationRules: make(map[Hostname]*Config),
	}

	r := sidecarConfig.Spec.(*networking.Sidecar)
	out.EgressListeners = make([]*IstioEgressListenerWrapper, 0)

	for _, e := range r.Egress {
		out.EgressListeners = append(out.EgressListeners, convertIstioListenerToWrapper(ps, sidecarConfig, e))
	}

	// for _, e := range r.Ingress {
	// 	// TODO: These need to go into CDS as well
	// 	out.IngressListeners = append(out.IngressListeners, convertIstioListenerToWrapper(ps, sidecarConfig, e))
	// }

	// Now collect all the imported services across all egress listeners. This is needed to generate CDS output
	servicesAdded := make(map[Hostname]struct{})
	dummyNode := Proxy{
		ConfigNamespace: sidecarConfig.Namespace,
	}

	for _, listener := range out.EgressListeners {
		for _, s := range listener.importedServices {
			// TODO: port merging when each listener generates a partial service
			if _, found := servicesAdded[s.Hostname]; !found {
				servicesAdded[s.Hostname] = struct{}{}
				out.allImportedServices = append(out.allImportedServices, s)
			}
		}
	}

	// Now that we have all the services that sidecars using this scope (in
	// this config namespace) will see, identify all the destinationRules and virtualServices
	// that these services need
	for _, s := range out.allImportedServices {
		out.allImportedDestinationRules[s.Hostname] = ps.DestinationRule(&dummyNode, s.Hostname)
	}
	meshGateway := map[string]bool{IstioMeshGateway: true}
	out.allImportedVirtualServices = out.selectVirtualServices(ps.VirtualServices(&dummyNode, meshGateway))
	return out
}

// Services returns the list of services imported across all egress listeners by this
// Sidecar config
func (sc *SidecarScope) Services() []*Service {
	if sc == nil {
		return nil
	}

	return sc.allImportedServices
}

// VirtualServices returns the list of virtual services imported by this Sidecar config
// across all egress listeners
func (sc *SidecarScope) VirtualServices() []Config {
	if sc == nil {
		return nil
	}

	return sc.allImportedVirtualServices
}

// DestinationRule returns the destination rule applicable for a given hostname
// used by CDS code
func (sc *SidecarScope) DestinationRule(hostname Hostname) *Config {
	if sc == nil {
		return nil
	}

	return sc.allImportedDestinationRules[hostname]
}

// Services returns the list of services imported by this egress listener
func (ilw *IstioEgressListenerWrapper) Services() []*Service {
	if ilw == nil {
		return nil
	}

	return ilw.importedServices
}

func convertIstioListenerToWrapper(ps *PushContext, sidecarConfig *Config,
	istioListener *networking.IstioEgressListener) *IstioEgressListenerWrapper {

	out := &IstioEgressListenerWrapper{
		IstioListener: istioListener,
		importMap:     make(map[string]Hostname),
	}

	if istioListener.Port != nil {
		out.ListenerPort = &Port{
			Name:     istioListener.Port.Name,
			Port:     int(istioListener.Port.Number),
			Protocol: ParseProtocol(istioListener.Port.Protocol),
		}
	}

	if istioListener.Hosts != nil {
		for _, h := range istioListener.Hosts {
			parts := strings.SplitN(h, "/", 2)
			out.importMap[parts[0]] = Hostname(parts[1])
		}
	}

	dummyNode := Proxy{
		ConfigNamespace: sidecarConfig.Namespace,
	}

	out.importedServices = out.selectServices(ps.Services(&dummyNode))

	return out
}

// GetEgressListenerForPort returns the egress listener corresponding to
// the listener port or the catch all listener
func (sc *SidecarScope) getEgressListenerForPort(port int) *IstioEgressListenerWrapper {
	if sc == nil {
		return nil
	}

	for _, e := range sc.EgressListeners {
		if e.ListenerPort == nil || e.ListenerPort.Port == port {
			return e
		}
	}
	return nil
}

// selectVirtualServices returns the list of virtual services selected
// across all egress listeners
func (sc *SidecarScope) selectVirtualServices(configs []Config) []Config {

	importedConfigs := make([]Config, 0)
	for _, c := range configs {
		configNamespace := c.Namespace
		rule := c.Spec.(*networking.VirtualService)
		for _, ilw := range sc.EgressListeners {
			// Check if there is an explicit import of form ns/* or ns/host
			if hostMatch, nsFound := ilw.importMap[configNamespace]; nsFound {
				// Check if the hostnames match per usual hostname matching rules
				hostFound := false
				for _, h := range rule.Hosts {
					// TODO: This is a bug. VirtualServices can have many hosts
					// while the user might be importing only a single host
					// We need to generate a new VirtualService with just the matched host
					if hostMatch.Matches(Hostname(h)) {
						importedConfigs = append(importedConfigs, c)
						hostFound = true
						break
					}
				}
				if hostFound {
					break
				}
			}

			// Check if there is an import of form */host or */*
			if hostMatch, wnsFound := ilw.importMap[wildcardNamespace]; wnsFound {
				// Check if the hostnames match per usual hostname matching rules
				for _, h := range rule.Hosts {
					// TODO: This is a bug. VirtualServices can have many hosts
					// while the user might be importing only a single host
					// We need to generate a new VirtualService with just the matched host
					if hostMatch.Matches(Hostname(h)) {
						importedConfigs = append(importedConfigs, c)
						break
					}
				}
			}
		}
	}

	return importedConfigs
}

// selectServices returns the list of services selected through the hosts field
// in the egress portion of the Sidecar config
func (ilw *IstioEgressListenerWrapper) selectServices(services []*Service) []*Service {

	importedServices := make([]*Service, 0)
	for _, s := range services {
		configNamespace := s.Attributes.Namespace
		// Check if there is an explicit import of form ns/* or ns/host
		if hostMatch, nsFound := ilw.importMap[configNamespace]; nsFound {
			// Check if the hostnames match per usual hostname matching rules
			if hostMatch.Matches(s.Hostname) {
				// TODO: See if the service's ports match.
				// If there is a listener port for this Listener, then
				//   check if the service has a port of same value.
				//   If not, check if the service has a single port - and choose that port
				//   if service has multiple ports none of which match the listener port, check if there is
				//   a virtualService with match Port
				importedServices = append(importedServices, s)
				continue
			}
			// hostname didn't match. Check if its imported as */host
		}

		// Check if there is an import of form */host or */*
		if hostMatch, wnsFound := ilw.importMap[wildcardNamespace]; wnsFound {
			// Check if the hostnames match per usual hostname matching rules
			if hostMatch.Matches(s.Hostname) {
				importedServices = append(importedServices, s)
			}
		}
	}

	return importedServices
}
