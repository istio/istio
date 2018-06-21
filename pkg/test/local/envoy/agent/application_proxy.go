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
	"istio.io/istio/pilot/pkg/model"
)

// ApplicationProxy is a wrapper around an Envoy proxy that has been configured for a particular backend Application.
type ApplicationProxy interface {
	// GetConfig returns the ServiceEntry configuration for this proxied application.
	GetConfig() model.Config
	// GetAdminPort returns the Envoy administration port for this proxy.
	GetAdminPort() int
	// GetPorts returns a list of port mappings between Envoy and the backend application.
	GetPorts() []MappedPort
}

// MappedPort provides a single port mapping between an Envoy proxy and its backend application.
type MappedPort struct {
	Name            string
	Protocol        model.Protocol
	ProxyPort       int
	ApplicationPort int
}

// ApplicationProxyFactory is a function that manufactures ApplicationProxy instances.
type ApplicationProxyFactory func(serviceName string, app Application) (ApplicationProxy, StopFunc, error)
