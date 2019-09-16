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
	"regexp"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

var (
	fqdnPattern = regexp.MustCompile(`^(.+)\.(.+)\.svc\.cluster\.local$`)
)

// DestinationAnalyzer checks the destinations associated with each virtual service
type DestinationAnalyzer struct{}

type hostAndSubset struct {
	host   resource.Name
	subset string
}

// Name implements Analyzer
func (da *DestinationAnalyzer) Name() string {
	return "virtualservice.DestinationAnalyzer"
}

// Analyze implements Analyzer
func (da *DestinationAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts := initServiceEntryHostNames(ctx)

	// To avoid repeated iteration, precompute the set of existing destination host+subset combinations
	destHostsAndSubsets := initDestHostsAndSubsets(ctx)

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		da.analyzeVirtualService(r, ctx, serviceEntryHosts, destHostsAndSubsets)
		return true
	})
}

func (da *DestinationAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context,
	serviceEntryHosts map[resource.Name]bool, destHostsAndSubsets map[hostAndSubset]bool) {

	vs := r.Item.(*v1alpha3.VirtualService)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	destinations := getRouteDestinations(vs)

	for _, destination := range destinations {
		if !da.checkDestinationHost(ns, destination, ctx, serviceEntryHosts) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewReferencedResourceNotFound(r, "host", destination.GetHost()))
			continue
		}
		if !da.checkDestinationSubset(ns, destination, destHostsAndSubsets) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewReferencedResourceNotFound(r, "host+subset", fmt.Sprintf("%s+%s", destination.GetHost(), destination.GetSubset())))
		}
	}
}

func (da *DestinationAnalyzer) checkDestinationHost(vsNamespace string, destination *v1alpha3.Destination,
	ctx analysis.Context, serviceEntryHosts map[resource.Name]bool) bool {

	name := getResourceNameFromHost(vsNamespace, destination.GetHost())

	// Check explicitly defined ServiceEntries as well as services discovered from the platform
	if _, ok := serviceEntryHosts[name]; ok {
		return true
	}
	if ctx.Exists(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, name) {
		return true
	}

	return false
}

func (da *DestinationAnalyzer) checkDestinationSubset(vsNamespace string, destination *v1alpha3.Destination, destHostsAndSubsets map[hostAndSubset]bool) bool {
	name := getResourceNameFromHost(vsNamespace, destination.GetHost())

	subset := destination.GetSubset()

	// if there's no subset specified, we're done
	if subset == "" {
		return true
	}

	hs := hostAndSubset{
		host:   name,
		subset: subset,
	}
	if _, ok := destHostsAndSubsets[hs]; ok {
		return true
	}

	return false
}

func initServiceEntryHostNames(ctx analysis.Context) map[resource.Name]bool {
	hosts := make(map[resource.Name]bool)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, func(r *resource.Entry) bool {
		s := r.Item.(*v1alpha3.ServiceEntry)
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		for _, h := range s.GetHosts() {
			hosts[getResourceNameFromHost(ns, h)] = true
		}
		return true
	})
	return hosts
}

func initDestHostsAndSubsets(ctx analysis.Context) map[hostAndSubset]bool {
	hostsAndSubsets := make(map[hostAndSubset]bool)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Destinationrules, func(r *resource.Entry) bool {
		dr := r.Item.(*v1alpha3.DestinationRule)
		drNamespace, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

		for _, ss := range dr.GetSubsets() {
			hs := hostAndSubset{
				host:   getResourceNameFromHost(drNamespace, dr.GetHost()),
				subset: ss.GetName(),
			}
			hostsAndSubsets[hs] = true
		}
		return true
	})
	return hostsAndSubsets
}

// getResourceNameFromHost figures out the resource.Name to look up from the provided host string
// We need to handle two possible formats: short name and FQDN
// https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#Destination
func getResourceNameFromHost(defaultNamespace, host string) resource.Name {

	// First, try to parse as FQDN (which can be cross-namespace)
	namespace, name := getNamespaceAndNameFromFQDN(host)

	//Otherwise, treat this as a short name and use the assumed namespace
	if namespace == "" {
		namespace = defaultNamespace
		name = host
	}
	return resource.NewName(namespace, name)
}

func getNamespaceAndNameFromFQDN(fqdn string) (string, string) {
	result := fqdnPattern.FindAllStringSubmatch(fqdn, -1)
	if len(result) == 0 {
		return "", ""
	}
	return result[0][2], result[0][1]
}

func getRouteDestinations(vs *v1alpha3.VirtualService) []*v1alpha3.Destination {
	destinations := make([]*v1alpha3.Destination, 0)

	for _, r := range vs.GetTcp() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}
	for _, r := range vs.GetTls() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}
	for _, r := range vs.GetHttp() {
		for _, rd := range r.GetRoute() {
			destinations = append(destinations, rd.GetDestination())
		}
	}

	return destinations
}
