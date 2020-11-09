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

package util

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

func InitServiceEntryHostMap(ctx analysis.Context) map[ScopedFqdn]*v1alpha3.ServiceEntry {
	result := make(map[ScopedFqdn]*v1alpha3.ServiceEntry)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), func(r *resource.Instance) bool {
		s := r.Message.(*v1alpha3.ServiceEntry)
		hostsNamespaceScope := string(r.Metadata.FullName.Namespace)
		if IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = ExportToAllNamespaces
		}
		for _, h := range s.GetHosts() {
			result[NewScopedFqdn(hostsNamespaceScope, r.Metadata.FullName.Namespace, h)] = s
		}
		return true
	})

	// converts k8s service to serviceEntry since destinationHost
	// validation is performed against serviceEntry
	ctx.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		s := r.Message.(*corev1.ServiceSpec)
		var se *v1alpha3.ServiceEntry
		hostsNamespaceScope, ok := r.Metadata.Annotations[annotation.NetworkingExportTo.Name]
		if !ok {
			hostsNamespaceScope = ExportToAllNamespaces
		}
		var ports []*v1alpha3.Port
		for _, p := range s.Ports {
			ports = append(ports, &v1alpha3.Port{
				Number:   uint32(p.Port),
				Name:     p.Name,
				Protocol: string(p.Protocol),
			})
		}
		host := ConvertHostToFQDN(r.Metadata.FullName.Namespace, r.Metadata.FullName.Name.String())
		se = &v1alpha3.ServiceEntry{
			Hosts: []string{host},
			Ports: ports,
		}
		result[NewScopedFqdn(hostsNamespaceScope, r.Metadata.FullName.Namespace, r.Metadata.FullName.Name.String())] = se
		return true

	})
	return result
}

func GetDestinationHost(sourceNs resource.Namespace, host string, serviceEntryHosts map[ScopedFqdn]*v1alpha3.ServiceEntry) *v1alpha3.ServiceEntry {
	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := NewScopedFqdn(string(sourceNs), sourceNs, host)
	if s, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		return s
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := NewScopedFqdn(ExportToAllNamespaces, sourceNs, host)
	if s, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		return s
	}

	// Now check wildcard matches, namespace scoped or all namespaces
	// (This more expensive checking left for last)
	// Assumes the wildcard entries are correctly formatted ("*<dns suffix>")
	for seHostScopedFqdn, s := range serviceEntryHosts {
		scope, seHost := seHostScopedFqdn.GetScopeAndFqdn()

		// Skip over non-wildcard entries
		if !strings.HasPrefix(seHost, Wildcard) {
			continue
		}

		// Skip over entries not visible to the current virtual service namespace
		if scope != ExportToAllNamespaces && scope != string(sourceNs) {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, Wildcard)
		hostWithoutWildCard := strings.TrimPrefix(host, Wildcard)

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			return s
		}
	}

	return nil
}
