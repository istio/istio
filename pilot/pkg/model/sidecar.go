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
	EgressListeners []*IstioListenerWrapper

	// set of ingress listeners
	// A sidecar scope should have either ingress/egress listeners or both.
	IngressListeners []*IstioListenerWrapper
}

// IstioListenerWrapper is a wrapper for networking.IstioListener object.
// It has the parsed form of the hosts field, encompassing a list of services
// per sidecar listener object.
type IstioListenerWrapper struct {
	// The actual IstioListener api object from the Config. It can be nil if
	// this is for the default sidecar scope.
	IstioListener *networking.IstioListener

	// TODO: Unix domain socket

	// optional. The port on which this listener should be configured. If
	// omitted, we infer from services imported
	Port *Port

	// REQUIRED: Namespaces and services/virtualservices imported
	importMap map[string]Hostname
}

// DefaultSidecarScope is a sidecar scope object with a default
// catch all egress listener
func DefaultSidecarScope() *SidecarScope {
	return &SidecarScope{
		EgressListeners: []*IstioListenerWrapper{
			{
				importMap: map[string]Hostname{wildcardNamespace: wildcardService},
			},
		},
	}
}

// ConvertToSidecarScope converts from Sidecar config to SidecarScope object
func ConvertToSidecarScope(sidecarConfig *Config) *SidecarScope {
	out := &SidecarScope{
		Config: sidecarConfig,
	}

	r := sidecarConfig.Spec.(*networking.Sidecar)
	out.EgressListeners = make([]*IstioListenerWrapper, len(r.Egress))
	out.IngressListeners = make([]*IstioListenerWrapper, len(r.Ingress))
	for _, e := range r.Egress {
		out.EgressListeners = append(out.EgressListeners, convertIstioListenerToWrapper(e))
	}
	for _, e := range r.Ingress {
		out.IngressListeners = append(out.IngressListeners, convertIstioListenerToWrapper(e))
	}

	return out
}

func convertIstioListenerToWrapper(istioListener *networking.IstioListener) *IstioListenerWrapper {
	out := &IstioListenerWrapper{
		IstioListener: istioListener,
		importMap:     make(map[string]Hostname),
	}

	if istioListener.Port != nil {
		out.Port = &Port{
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

	return out
}

// GetEgressListenerForPort returns the egress listener corresponding to
// the listener port or the catch all listener
func (sc *SidecarScope) GetEgressListenerForPort(port int) *IstioListenerWrapper {
	for _, e := range sc.EgressListeners {
		if e.Port == nil || e.Port.Port == port {
			return e
		}
	}
	return nil
}

// SelectServices returns the list of services selected through the hosts field
// in the ingress/egress portion of the Sidecar config
func (ilw *IstioListenerWrapper) SelectServices(services []*Service) []*Service {
	importedServices := make([]*Service, 0)
	for _, s := range services {
		configNamespace := s.Attributes.Namespace
		// Check if there is an explicit import of form ns/* or ns/host
		// else check if there is an import of form */host or */*
		if hostMatch, nsFound := ilw.importMap[configNamespace]; nsFound {
			// Check if the hostnames match per usual hostname matching rules
			if hostMatch.Matches(s.Hostname) {
				importedServices = append(importedServices, s)
			}
		} else if hostMatch, wnsFound := ilw.importMap[wildcardNamespace]; wnsFound {
			// Check if there is an import of form */host or */*

			// Check if the hostnames match per usual hostname matching rules
			if hostMatch.Matches(s.Hostname) {
				importedServices = append(importedServices, s)
			}
		}
	}

	return importedServices
}

// SelectVirtualServices returns the list of virtual services selected
// through the hosts field in the ingress/egress portion of the Sidecar
// config
func (ilw *IstioListenerWrapper) SelectVirtualServices(configs []Config) []Config {
	importedConfigs := make([]Config, 0)
	for _, c := range configs {
		configNamespace := c.Namespace
		rule := c.Spec.(*networking.VirtualService)
		// Check if there is an explicit import of form ns/* or ns/host
		if hostMatch, nsFound := ilw.importMap[configNamespace]; nsFound {
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
		} else if hostMatch, wnsFound := ilw.importMap[wildcardNamespace]; wnsFound {
			// Check if there is an import of form */host or */*

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

	return importedConfigs
}
