// Copyright 2017 Istio Authors
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

package proxy

import (
	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
)

// Context defines local proxy context information about the global service mesh
type Context struct {
	// Discovery interface for listing services and instances
	Discovery model.ServiceDiscovery

	// Accounts interface for listing service accounts
	Accounts model.ServiceAccounts

	// Config interface for listing routing rules
	Config *model.IstioRegistry

	// MeshConfig defines global configuration settings
	MeshConfig *proxyconfig.ProxyMeshConfig

	// IPAddress is the IP address of the proxy used to identify it and its
	// co-located service instances. Example: "10.60.1.6"
	IPAddress string

	// UID is the platform specific unique identifier of the proxy.
	// UID should serve as a key to lookup additional information associated with the
	// proxy. Example: "kubernetes://my-pod.my-namespace"
	UID string

	// PassthroughPorts is a list of ports on the proxy IP address that must be
	// open and allowed through the proxy to the co-located service instances.
	// These ports are utilized by the underlying cluster platform for health
	// checking, for example.
	//
	// The passthrough ports should be exposed irrespective of the services
	// model. In case there is an overlap, that is the port is utilized by a
	// service for a service instance and is also present in this list, the
	// service model declaration takes precedence. That means any protocol
	// upgrade (such as utilizng TLS for proxy-to-proxy traffic) will be applied
	// to the passthrough port.
	PassthroughPorts []int
}
