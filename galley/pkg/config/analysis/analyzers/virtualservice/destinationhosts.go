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

package virtualservice

import (
	"fmt"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// DestinationHostAnalyzer checks the destination hosts associated with each virtual service
type DestinationHostAnalyzer struct{}

var _ analysis.Analyzer = &DestinationHostAnalyzer{}

type hostAndSubset struct {
	host   resource.FullName
	subset string
}

// Metadata implements Analyzer
func (a *DestinationHostAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.DestinationHostAnalyzer",
		Description: "Checks the destination hosts associated with each virtual service",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Serviceentries.Name(),
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

// Analyze implements Analyzer
func (a *DestinationHostAnalyzer) Analyze(ctx analysis.Context) {
	// Precompute the set of service entry hosts that exist (there can be more than one defined per ServiceEntry CRD)
	serviceEntryHosts := util.InitServiceEntryHostMap(ctx)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		a.analyzeVirtualService(r, ctx, serviceEntryHosts)
		return true
	})
}

func (a *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Instance, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry) {

	vs := r.Message.(*v1alpha3.VirtualService)

	for _, d := range getRouteDestinations(vs) {
		s := util.GetDestinationHost(r.Metadata.FullName.Namespace, d.GetHost(), serviceEntryHosts)
		if s == nil {
			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewReferencedResourceNotFound(r, "host", d.GetHost()))
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}

	for _, d := range getHTTPMirrorDestinations(vs) {
		s := util.GetDestinationHost(r.Metadata.FullName.Namespace, d.GetHost(), serviceEntryHosts)
		if s == nil {
			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
				msg.NewReferencedResourceNotFound(r, "mirror host", d.GetHost()))
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}
}

func checkServiceEntryPorts(ctx analysis.Context, r *resource.Instance, d *v1alpha3.Destination, s *v1alpha3.ServiceEntry) {
	if d.GetPort() == nil {
		// If destination port isn't specified, it's only a problem if the service being referenced exposes multiple ports.
		if len(s.GetPorts()) > 1 {
			var portNumbers []int
			for _, p := range s.GetPorts() {
				portNumbers = append(portNumbers, int(p.GetNumber()))
			}
			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
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
		ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			msg.NewReferencedResourceNotFound(r, "host:port", fmt.Sprintf("%s:%d", d.GetHost(), d.GetPort().GetNumber())))
	}
}
