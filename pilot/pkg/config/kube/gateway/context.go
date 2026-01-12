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

	corev1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/sets"
)

// GatewayContext contains a minimal subset of push context functionality to be exposed to GatewayAPIControllers
type GatewayContext struct {
	ps      *model.PushContext
	cluster cluster.ID
}

func NewGatewayContext(ps *model.PushContext, cluster cluster.ID) GatewayContext {
	return GatewayContext{ps, cluster}
}

// ResolveGatewayInstances attempts to resolve all instances that a gateway will be exposed on.
// Note: this function considers *all* instances of the service; its possible those instances will not actually be properly functioning
// gateways, so this is not 100% accurate, but sufficient to expose intent to users.
// The actual configuration generation is done on a per-workload basis and will get the exact set of matched instances for that workload.
// Four sets are exposed:
// * Internal addresses (eg istio-ingressgateway.istio-system.svc.cluster.local:80).
// * Internal IP addresses (eg 1.2.3.4). This comes from ClusterIP.
// * External addresses (eg 1.2.3.4), this comes from LoadBalancer services. There may be multiple in some cases (especially multi cluster).
// * Pending addresses (eg istio-ingressgateway.istio-system.svc), are LoadBalancer-type services with pending external addresses.
// * Warnings for references that could not be resolved. These are intended to be user facing.
func (gc GatewayContext) ResolveGatewayInstances(
	namespace string,
	gwsvcs []string,
	servers []*networking.Server,
) (internal, internalIP, external, pending, warns []string, allUsable bool) {
	ports := map[int]struct{}{}
	for _, s := range servers {
		ports[int(s.Port.Number)] = struct{}{}
	}
	foundInternal := sets.New[string]()
	foundInternalIP := sets.New[string]()
	foundExternal := sets.New[string]()
	foundPending := sets.New[string]()
	warnings := []string{}
	foundUnusable := false
	log.Debugf("Resolving gateway instances for %v in namespace %s", gwsvcs, namespace)
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
			foundUnusable = true
			continue
		}
		svcKey := svc.Key()
		for port := range ports {
			instances := gc.ps.ServiceEndpointsByPort(svc, port, nil)
			if len(instances) > 0 {
				foundInternal.Insert(g + ":" + strconv.Itoa(port))
				dummyProxy := &model.Proxy{Metadata: &model.NodeMetadata{ClusterID: gc.cluster}}
				dummyProxy.SetIPMode(model.Dual)
				foundInternalIP.InsertAll(svc.GetAllAddressesForProxy(dummyProxy)...)
				if svc.Attributes.ClusterExternalAddresses.Len() > 0 {
					// Fetch external IPs from all clusters
					svc.Attributes.ClusterExternalAddresses.ForEach(func(c cluster.ID, externalIPs []string) {
						foundExternal.InsertAll(externalIPs...)
					})
				} else if corev1.ServiceType(svc.Attributes.Type) == corev1.ServiceTypeLoadBalancer {
					if !foundPending.Contains(g) {
						warnings = append(warnings, fmt.Sprintf("address pending for hostname %q", g))
						foundPending.Insert(g)
					}
				}
			} else {
				instancesByPort := gc.ps.ServiceEndpoints(svcKey)
				if instancesEmpty(instancesByPort) {
					warnings = append(warnings, fmt.Sprintf("no instances found for hostname %q", g))
				} else {
					hintPort := sets.New[string]()
					for servicePort, instances := range instancesByPort {
						for _, i := range instances {
							if i.EndpointPort == uint32(port) {
								hintPort.Insert(strconv.Itoa(servicePort))
							}
						}
					}
					if hintPort.Len() > 0 {
						warnings = append(warnings, fmt.Sprintf(
							"port %d not found for hostname %q (hint: the service port should be specified, not the workload port. Did you mean one of these ports: %v?)",
							port, g, sets.SortedList(hintPort)))
						foundUnusable = true
					} else {
						_, isManaged := svc.Attributes.Labels[label.GatewayManaged.Name]
						var portExistsOnService bool
						for _, p := range svc.Ports {
							if p.Port == port {
								portExistsOnService = true
								break
							}
						}
						// If this is a managed gateway, the only possible explanation for no instances for the port
						// is a delay in endpoint sync. Therefore, we don't want to warn/change the Programmed condition
						// in this case as long as the port exists on the `Service` object.
						if !isManaged || !portExistsOnService {
							warnings = append(warnings, fmt.Sprintf("port %d not found for hostname %q", port, g))
							foundUnusable = true
						}
					}
				}
			}
		}
	}
	sort.Strings(warnings)
	return sets.SortedList(foundInternal), sets.SortedList(foundInternalIP), sets.SortedList(foundExternal), sets.SortedList(foundPending),
		warnings, !foundUnusable
}

func (gc GatewayContext) GetService(hostname, namespace string) *model.Service {
	return gc.ps.ServiceIndex.HostnameAndNamespace[host.Name(hostname)][namespace]
}

func instancesEmpty(m map[int][]*model.IstioEndpoint) bool {
	for _, instances := range m {
		if len(instances) > 0 {
			return false
		}
	}
	return true
}
