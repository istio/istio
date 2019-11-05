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
func (d *DestinationHostAnalyzer) Metadata() analysis.Metadata {
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
func (d *DestinationHostAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts := initServiceEntryHostMap(ctx)

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		d.analyzeVirtualService(r, ctx, serviceEntryHosts)
		return true
	})
}

func (d *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {

	vs := r.Item.(*v1alpha3.VirtualService)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	destinations := getRouteDestinations(vs)

	for _, destination := range destinations {
		if !d.checkDestinationHost(ns, destination, ctx, serviceEntryHosts) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewReferencedResourceNotFound(r, "host", destination.GetHost()))
		}
		d.checkDestinationPort(r, ns, destination, ctx, serviceEntryHosts)
	}
}

func getServiceEntry(nsScopedFqdn util.ScopedFqdn, ctx analysis.Context) *resource.Entry {
	_, fqdn := nsScopedFqdn.GetScopeAndFqdn()
	n := util.GetResourceNameFromHost("", fqdn)
	return ctx.Find(metadata.IstioNetworkingV1Alpha3Serviceentries, n)
}

func checkServiceEntryPorts(r *resource.Entry, s *v1alpha3.ServiceEntry, d *v1alpha3.Destination, ctx analysis.Context) {
	port := d.GetPort()
	if port == nil {
		if len(s.GetPorts()) > 1 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.NewInternalError(r, "TODO: Must specify port if targeted service has more than one"))
			return
		}
		// Nothing to check, and that's OK
		return
	}
	foundPort := false
	for _, p := range s.GetPorts() {
		if port.GetNumber() == p.GetNumber() {
			foundPort = true
			break
		}
	}
	if !foundPort {
		ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.NewInternalError(r, "TODO: Could not find matching port on target service"))
		return
	}
}

// TODO: Refactor to avoid duplicating work with checkDestinationHost
func (d *DestinationHostAnalyzer) checkDestinationPort(r *resource.Entry, vsNamespace string, destination *v1alpha3.Destination,
	ctx analysis.Context, serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {
	host := destination.GetHost()

	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := util.NewScopedFqdn(vsNamespace, vsNamespace, host)
	if s, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		checkServiceEntryPorts(r, s, destination, ctx)
		return
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, vsNamespace, host)
	if s, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		checkServiceEntryPorts(r, s, destination, ctx)
		return
	}

	// Check synthetic service entries (service discovery services)
	name := util.GetResourceNameFromHost(vsNamespace, host)
	se := ctx.Find(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, name)
	if se != nil {
		checkServiceEntryPorts(r, se.Item.(*v1alpha3.ServiceEntry), destination, ctx)
		return
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
		if scope != util.ExportToAllNamespaces && scope != vsNamespace {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, util.Wildcard)
		hostWithoutWildCard := strings.TrimPrefix(host, util.Wildcard)

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			checkServiceEntryPorts(r, s, destination, ctx)
			return
		}
	}
}

func (d *DestinationHostAnalyzer) checkDestinationHost(vsNamespace string, destination *v1alpha3.Destination,
	ctx analysis.Context, serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) bool {
	host := destination.GetHost()

	// Check explicitly defined ServiceEntries as well as services discovered from the platform

	// ServiceEntries can be either namespace scoped or exposed to all namespaces
	nsScopedFqdn := util.NewScopedFqdn(vsNamespace, vsNamespace, host)
	if _, ok := serviceEntryHosts[nsScopedFqdn]; ok {
		return true
	}

	// Check ServiceEntries which are exposed to all namespaces
	allNsScopedFqdn := util.NewScopedFqdn(util.ExportToAllNamespaces, vsNamespace, host)
	if _, ok := serviceEntryHosts[allNsScopedFqdn]; ok {
		return true
	}

	// Check synthetic service entries (service discovery services)
	name := util.GetResourceNameFromHost(vsNamespace, host)
	if ctx.Exists(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, name) {
		return true
	}

	// Now check wildcard matches, namespace scoped or all namespaces
	// (This more expensive checking left for last)
	// Assumes the wildcard entries are correctly formatted ("*<dns suffix>")
	for seHostScopedFqdn := range serviceEntryHosts {
		scope, seHost := seHostScopedFqdn.GetScopeAndFqdn()

		// Skip over non-wildcard entries
		if !strings.HasPrefix(seHost, util.Wildcard) {
			continue
		}

		// Skip over entries not visible to the current virtual service namespace
		if scope != util.ExportToAllNamespaces && scope != vsNamespace {
			continue
		}

		seHostWithoutWildcard := strings.TrimPrefix(seHost, util.Wildcard)
		hostWithoutWildCard := strings.TrimPrefix(host, util.Wildcard)

		if strings.HasSuffix(hostWithoutWildCard, seHostWithoutWildcard) {
			return true
		}
	}

	return false
}

//TODO: Need to map to the actual service entry for easy lookup
func initServiceEntryHostMap(ctx analysis.Context) map[util.ScopedFqdn]*v1alpha3.ServiceEntry {
	hosts := make(map[util.ScopedFqdn]*v1alpha3.ServiceEntry)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, func(r *resource.Entry) bool {
		s := r.Item.(*v1alpha3.ServiceEntry)
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		hostsNamespaceScope := ns
		if util.IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = util.ExportToAllNamespaces
		}
		for _, h := range s.GetHosts() {
			hosts[util.NewScopedFqdn(hostsNamespaceScope, ns, h)] = s
		}
		return true
	})
	return hosts
}
