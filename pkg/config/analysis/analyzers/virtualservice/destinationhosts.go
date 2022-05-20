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
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
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
	virtualServiceDestinations := initVirtualServiceDestinations(ctx)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		a.analyzeVirtualService(r, ctx, serviceEntryHosts)
		a.analyzeSubset(r, ctx, virtualServiceDestinations)
		return true
	})
}

func (a *DestinationHostAnalyzer) analyzeSubset(r *resource.Instance, ctx analysis.Context, vsDestinations map[resource.FullName][]*v1alpha3.Destination) {
	vs := r.Message.(*v1alpha3.VirtualService)

	// if there's no gateway specified, we're done
	if len(vs.Gateways) == 0 {
		return
	}

	for ruleIndex, http := range vs.Http {
		for routeIndex, route := range http.Route {
			if route.Destination.Subset == "" {
				for virtualservice, destinations := range vsDestinations {
					for _, destination := range destinations {
						if destination.Host == route.Destination.Host {
							m := msg.NewIngressRouteRulesNotAffected(r, virtualservice.String(), r.Metadata.FullName.String())

							key := fmt.Sprintf(util.DestinationHost, http.Name, ruleIndex, routeIndex)
							if line, ok := util.ErrorLine(r, key); ok {
								m.Line = line
							}

							ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
						}
					}
				}
			}
		}
	}
}

// get all virtualservice that have destination with subset
func initVirtualServiceDestinations(ctx analysis.Context) map[resource.FullName][]*v1alpha3.Destination {
	virtualservices := make(map[resource.FullName][]*v1alpha3.Destination)

	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		virtualservice := r.Message.(*v1alpha3.VirtualService)
		for _, routes := range virtualservice.Http {
			for _, destinations := range routes.Route {
				// if there's no subset specified, we're done
				if destinations.Destination.Subset != "" {
					for _, host := range virtualservice.Hosts {
						if destinations.Destination.Host == host {
							virtualservices[r.Metadata.FullName] = append(virtualservices[r.Metadata.FullName], destinations.Destination)
						}
					}
				}
			}
		}

		return true
	})

	return virtualservices
}

func (a *DestinationHostAnalyzer) analyzeVirtualService(r *resource.Instance, ctx analysis.Context,
	serviceEntryHosts map[util.ScopedFqdn]*v1alpha3.ServiceEntry,
) {
	vs := r.Message.(*v1alpha3.VirtualService)

	for _, d := range getRouteDestinations(vs) {
		s := util.GetDestinationHost(r.Metadata.FullName.Namespace, d.Destination.GetHost(), serviceEntryHosts)
		if s == nil {

			m := msg.NewReferencedResourceNotFound(r, "host", d.Destination.GetHost())

			key := fmt.Sprintf(util.DestinationHost, d.RouteRule, d.ServiceIndex, d.DestinationIndex)
			if line, found := util.ErrorLine(r, key); found {
				m.Line = line
			}

			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}

	for _, d := range getHTTPMirrorDestinations(vs) {
		s := util.GetDestinationHost(r.Metadata.FullName.Namespace, d.Destination.GetHost(), serviceEntryHosts)
		if s == nil {

			m := msg.NewReferencedResourceNotFound(r, "mirror host", d.Destination.GetHost())

			key := fmt.Sprintf(util.MirrorHost, d.ServiceIndex)
			if line, ok := util.ErrorLine(r, key); ok {
				m.Line = line
			}

			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
			continue
		}
		checkServiceEntryPorts(ctx, r, d, s)
	}
}

func checkServiceEntryPorts(ctx analysis.Context, r *resource.Instance, d *AnnotatedDestination, s *v1alpha3.ServiceEntry) {
	if d.Destination.GetPort() == nil {
		// If destination port isn't specified, it's only a problem if the service being referenced exposes multiple ports.
		if len(s.GetPorts()) > 1 {
			var portNumbers []int
			for _, p := range s.GetPorts() {
				portNumbers = append(portNumbers, int(p.GetNumber()))
			}

			m := msg.NewVirtualServiceDestinationPortSelectorRequired(r, d.Destination.GetHost(), portNumbers)

			if d.RouteRule == "http.mirror" {
				key := fmt.Sprintf(util.MirrorHost, d.ServiceIndex)
				if line, ok := util.ErrorLine(r, key); ok {
					m.Line = line
				}
			} else {
				key := fmt.Sprintf(util.DestinationHost, d.RouteRule, d.ServiceIndex, d.DestinationIndex)
				if line, ok := util.ErrorLine(r, key); ok {
					m.Line = line
				}
			}

			ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
			return
		}

		// Otherwise, it's not needed and we're done here.
		return
	}

	foundPort := false
	for _, p := range s.GetPorts() {
		if d.Destination.GetPort().GetNumber() == p.GetNumber() {
			foundPort = true
			break
		}
	}
	if !foundPort {

		m := msg.NewReferencedResourceNotFound(r, "host:port",
			fmt.Sprintf("%s:%d", d.Destination.GetHost(), d.Destination.GetPort().GetNumber()))

		if d.RouteRule == "http.mirror" {
			key := fmt.Sprintf(util.MirrorHost, d.ServiceIndex)
			if line, ok := util.ErrorLine(r, key); ok {
				m.Line = line
			}
		} else {
			key := fmt.Sprintf(util.DestinationHost, d.RouteRule, d.ServiceIndex, d.DestinationIndex)
			if line, ok := util.ErrorLine(r, key); ok {
				m.Line = line
			}
		}

		ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
	}
}
