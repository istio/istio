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

package agent

import (
	"fmt"
	"io"

	"istio.io/istio/pkg/test/application"

	"istio.io/istio/pilot/pkg/model"
)

// Agent is a wrapper around an Envoy proxy that has been configured for a particular backend Application.
type Agent interface {
	io.Closer
	// GetConfig returns the ServiceEntry configuration for this agent.
	GetConfig() model.Config
	// GetAdminPort returns the Envoy administration port for this agent.
	GetAdminPort() int
	// GetPorts returns a list of port mappings between Envoy and the backend application.
	GetPorts() []*MappedPort
	// IsConfiguredForService indicates whether this agent has received configuration for communication with the given service.
	CheckConfiguredForService(target Agent) error
}

// MappedPort provides a single port mapping between an Envoy proxy and its backend application.
type MappedPort struct {
	Name            string
	Protocol        model.Protocol
	ProxyPort       int
	ApplicationPort int
}

// Factory is a function that manufactures Agent instances.
type Factory func(meta model.ConfigMeta, appFactory application.Factory, configStore model.ConfigStore) (Agent, error)

// FindFirstPortForProtocol is a utility method to simplify lookup of a port for a given protocol.
func FindFirstPortForProtocol(a Agent, protocol model.Protocol) (*MappedPort, error) {
	for _, port := range a.GetPorts() {
		if port.Protocol == protocol {
			return port, nil
		}
	}
	return nil, fmt.Errorf("no port found matching protocol %v", protocol)
}
