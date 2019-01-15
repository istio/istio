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

package serviceregistry

// ServiceRegistry defines underlying platform supporting service registry
type ServiceRegistry string

const (
	// MockRegistry is a service registry that contains 2 hard-coded test services
	MockRegistry ServiceRegistry = "Mock"
	// KubernetesRegistry is a service registry backed by k8s API server
	KubernetesRegistry ServiceRegistry = "Kubernetes"
	// ConsulRegistry is a service registry backed by Consul
	ConsulRegistry ServiceRegistry = "Consul"
	// MCPRegistry is a service registry backed by MCP ServiceEntries
	MCPRegistry ServiceRegistry = "MCP"
)
