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
	// co-located service instances
	IPAddress string
}
