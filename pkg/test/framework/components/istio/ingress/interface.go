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

package ingress

import (
	"net/netip"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
)

type Instances []Instance

func (i Instances) Callers() echo.Callers {
	var out echo.Callers
	for _, instance := range i {
		out = append(out, instance)
	}
	return out
}

// Instance represents a deployed Ingress Gateway instance.
type Instance interface {
	echo.Caller
	// HTTPAddresses returns the external HTTP (80) address of the ingress gateway ((or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	HTTPAddresses() ([]string, []int)
	// HTTPSAddresses returns the external HTTPS (443) address of the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	HTTPSAddresses() ([]string, []int)
	// TCPAddresses returns the external TCP (31400) address of the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	TCPAddresses() ([]string, []int)
	// DiscoveryAddresses returns the external XDS (15012) address on the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	DiscoveryAddresses() []netip.AddrPort
	// HTTPAddresses returns the external HBONE (15008) address of the ingress gateway ((or the NodePort address,
	// when in an environment that doesn't support LoadBalancer).
	HBONEAddresses() ([]string, []int)
	// AddressesForPort returns the external address of the ingress gateway (or the NodePort address,
	// when in an environment that doesn't support LoadBalancer) for the given port.
	AddressesForPort(port int) ([]string, []int)

	// PodID returns the name of the ingress gateway pod of index i. Returns error if failed to get the pod
	// or the index is out of boundary.
	PodID(i int) (string, error)

	// Cluster the ingress is deployed to
	Cluster() cluster.Cluster

	// ServiceName of the ingress
	ServiceName() string

	// Namespace of the ingress
	Namespace() string
}
