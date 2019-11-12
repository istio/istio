// Copyright 2019 Istio Authors
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

package virtualservice

import (
	"fmt"
	"strings"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// DestinationHostAnalyzer checks the destination hosts associated with each virtual service
type DestinationHostAnalyzer struct{}

var _ analysis.Analyzer = &DestinationHostAnalyzer{}

type hostAndSubset struct {
	host   resource.Name
	subset string
}

// Metadata implements Analyzer
func (a *DestinationHostAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "virtualservice.DestinationHostAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3SyntheticServiceentries,
			metadata.IstioNetworkingV1Alpha3Serviceentries,
			metadata.IstioNetworkingV1Alpha3Virtualservices,
		},
	}
}

// Analyze implements Analyzer
func (a *DestinationHostAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts := initServiceEntryHostMap(ctx)

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		a.analyzeVirtualService(r, ctx, serviceEntryHosts)
		return true
	})
}

func (a *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {

	vs := r.Item.(*v1alpha3.VirtualService)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	for _, d := range getRouteDestinations(vs) {
		s := getDestinationHost(ns, d.GetHost(), serviceEntryHosts)
		if s == nil {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewReferencedResourceNotFound(r, "host", d.GetHost()))
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}
}

func getDestinationHost(sourceNs, host string, serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) *v1alpha3.ServiceEntry {
	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := util.NewScopedFqdn(sourceNs, sourceNs, host)
	if s, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		return s
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, sourceNs, host)
	if s, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		return s
	}

	// Now check wildcard matches, namespace scoped or all namespaces
	// (This more expensive checking left for last)
	// Assumes the wildcard entries are correctly formatted ("*<dns suffix>")
	for seHostScopedFqdn, s := range serviceEntryHosts {
		scope, seHost := seHostScopedFqdn.GetScopeAndFqdn()

		// Skip over non-wildcard entries
		if !strings.HasPrefix(seHost, util.Wildcard) {
			continue
		}

		// Skip over entries not visible to the current virtual service namespace
		if scope != util.ExportToAllNamespaces && scope != sourceNs {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, util.Wildcard)
		hostWithoutWildCard := strings.TrimPrefix(host, util.Wildcard)

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			return s
		}
	}

	return nil
}

func initServiceEntryHostMap(ctx analysis.Context) map[util.ScopedFqdn]*v1alpha3.ServiceEntry {
	result := make(map[util.ScopedFqdn]*v1alpha3.ServiceEntry)

	extractFn := func(r *resource.Entry) bool {
		s := r.Item.(*v1alpha3.ServiceEntry)
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		hostsNamespaceScope := ns
		if util.IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = util.ExportToAllNamespaces
		}
		for _, h := range s.GetHosts() {
			result[util.NewScopedFqdn(hostsNamespaceScope, ns, h)] = s
		}
		return true
	}

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, extractFn)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, extractFn)

	return result
}

func checkServiceEntryPorts(ctx analysis.Context, r *resource.Entry, d *v1alpha3.Destination, s *v1alpha3.ServiceEntry) {
	if d.GetPort() == nil {
		// If destination port isn't specified, it's only a problem if the service being referenced exposes multiple ports.
		if len(s.GetPorts()) > 1 {
			var portNumbers []int
			for _, p := range s.GetPorts() {
				portNumbers = append(portNumbers, int(p.GetNumber()))
			}
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewVirtualServiceDestinationPortSelectorRequired(r, d.GetHost(), portNumbers))
			return
		}

		// Otherwise, it's not needed and we're done here.
		return
	}

	foundPort := false
	for _, p := range s.GetPorts() {
		if d.GetPort().GetNumber() == p.GetNumber() {
			foundPort = true
			break
		}
	}
	if !foundPort {
		ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
			msg.NewReferencedResourceNotFound(r, "host:port", fmt.Sprintf("%s:%d", d.GetHost(), d.GetPort().GetNumber())))
	}
}
