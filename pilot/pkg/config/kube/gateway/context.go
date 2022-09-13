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

package gateway

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/sets"
)

// GatewayContext contains a minimal subset of push context functionality to be exposed to GatewayAPIControllers
type GatewayContext struct {
	ps *model.PushContext
}

func NewGatewayContext(ps *model.PushContext) GatewayContext {
	return GatewayContext{ps}
}

// ResolveGatewayInstances attempts to resolve all instances that a gateway will be exposed on.
// Note: this function considers *all* instances of the service; its possible those instances will not actually be properly functioning
// gateways, so this is not 100% accurate, but sufficient to expose intent to users.
// The actual configuration generation is done on a per-workload basis and will get the exact set of matched instances for that workload.
// Three sets are exposed:
// * Internal addresses (ie istio-ingressgateway.istio-system.svc.cluster.local:80).
// * External addresses (ie 1.2.3.4), this comes from LoadBalancer services. There may be multiple in some cases (especially multi cluster).
// * Warnings for references that could not be resolved. These are intended to be user facing.
func (gc GatewayContext) ResolveGatewayInstances(namespace string, gwsvcs []string, servers []*networking.Server) (internal, external, warns []string) {
	ports := map[int]struct{}{}
	for _, s := range servers {
		ports[int(s.Port.Number)] = struct{}{}
	}
	foundInternal := sets.New()
	foundExternal := sets.New()
	warnings := []string{}
	for _, g := range gwsvcs {
		svc, f := gc.ps.ServiceIndex.HostnameAndNamespace[host.Name(g)][namespace]
		if !f {
			otherNamespaces := []string{}
			for ns := range gc.ps.ServiceIndex.HostnameAndNamespace[host.Name(g)] {
				otherNamespaces = append(otherNamespaces, `"`+ns+`"`) // Wrap in quotes for output
			}
			if len(otherNamespaces) > 0 {
				sort.Strings(otherNamespaces)
				warnings = append(warnings, fmt.Sprintf("hostname %q not found in namespace %q, but it was found in namespace(s) %v",
					g, namespace, strings.Join(otherNamespaces, ", ")))
			} else {
				warnings = append(warnings, fmt.Sprintf("hostname %q not found", g))
			}
			continue
		}
		svcKey := svc.Key()
		for port := range ports {
			instances := gc.ps.ServiceInstancesByPort(svc, port, nil)
			if len(instances) > 0 {
				foundInternal.Insert(fmt.Sprintf("%s:%d", g, port))
				// Fetch external IPs from all clusters
				svc.Attributes.ClusterExternalAddresses.ForEach(func(c cluster.ID, externalIPs []string) {
					foundExternal.InsertAll(externalIPs...)
				})
			} else {
				instancesByPort := gc.ps.ServiceInstances(svcKey)
				if instancesEmpty(instancesByPort) {
					warnings = append(warnings, fmt.Sprintf("no instances found for hostname %q", g))
				} else {
					hintPort := sets.New()
					for _, instances := range instancesByPort {
						for _, i := range instances {
							if i.Endpoint.EndpointPort == uint32(port) {
								hintPort.Insert(strconv.Itoa(i.ServicePort.Port))
							}
						}
					}
					if len(hintPort) > 0 {
						warnings = append(warnings, fmt.Sprintf(
							"port %d not found for hostname %q (hint: the service port should be specified, not the workload port. Did you mean one of these ports: %v?)",
							port, g, hintPort.SortedList()))
					} else {
						warnings = append(warnings, fmt.Sprintf("port %d not found for hostname %q", port, g))
					}
				}
			}
		}
	}
	sort.Strings(warnings)
	return foundInternal.SortedList(), foundExternal.SortedList(), warnings
}

func (gc GatewayContext) GetService(hostname, namespace string) *model.Service {
	return gc.ps.ServiceIndex.HostnameAndNamespace[host.Name(hostname)][namespace]
}

func instancesEmpty(m map[int][]*model.ServiceInstance) bool {
	for _, instances := range m {
		if len(instances) > 0 {
			return false
		}
	}
	return true
}
