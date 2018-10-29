//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package service

import (
	"fmt"

	istio_networking_api "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

const (
	// Namespace for services running in the local environment
	Namespace = "istio-system"
	// Domain for services running in the local environment
	Domain = "svc.local"
	// LocalIPAddress for connections to localhost
	LocalIPAddress = "127.0.0.1"
	// LocalCIDR for connections to localhost
	LocalCIDR = "127.0.0.1/32"
)

var (
	// FullyQualifiedDomainName for local services
	FullyQualifiedDomainName = fmt.Sprintf("%s.%s", Namespace, Domain)
)

// Manager is a wrapper around a model.ConfigStoreCache that simplifies service creation for the local environment.
type Manager struct {
	// ConfigStore for all deployments.
	ConfigStore model.ConfigStoreCache
}

// NewManager creates a new manager with an in-memory ConfigStore.
func NewManager() *Manager {
	return &Manager{
		ConfigStore: memory.NewController(memory.Make(model.IstioConfigTypes)),
	}
}

// Create a new ServiceEntry for the given service and adds it to the ConfigStore.
func (e *Manager) Create(serviceName, version string, ports model.PortList) (cfg model.Config, err error) {
	cfgPorts := make([]*istio_networking_api.Port, len(ports))
	for i, p := range ports {
		cfgPorts[i] = &istio_networking_api.Port{
			Name:     p.Name,
			Protocol: string(p.Protocol),
			Number:   uint32(p.Port),
		}
	}

	cfg = model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ServiceEntry.Type,
			Name:      serviceName,
			Namespace: Namespace,
			Domain:    Domain,
			Labels: map[string]string{
				"app":     serviceName,
				"version": version,
			},
		},
		Spec: &istio_networking_api.ServiceEntry{
			Hosts: []string{
				fmt.Sprintf("%s.%s", serviceName, FullyQualifiedDomainName),
			},
			Addresses: []string{
				LocalCIDR,
			},
			Resolution: istio_networking_api.ServiceEntry_STATIC,
			Location:   istio_networking_api.ServiceEntry_MESH_INTERNAL,
			Endpoints: []*istio_networking_api.ServiceEntry_Endpoint{
				{
					Address: LocalIPAddress,
				},
			},
			Ports: cfgPorts,
		},
	}

	_, err = e.ConfigStore.Create(cfg)
	return
}
