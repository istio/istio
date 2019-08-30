package virtualservice

import (
	"fmt"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

// DestinationAnalyzer checks the hosts associated with each virtual service
// TODO: This all needs lots of cleanup and correctness checking
type DestinationAnalyzer struct {
	// ServiceNames      []string
	// ServiceEntryHosts []string
}

// Name implements Analyzer
func (da *DestinationAnalyzer) Name() string {
	return "virtualservice.DestinationAnalyzer"
}

// Analyze implements Analyzer
func (da *DestinationAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		da.analyzeVirtualService(r, ctx)
		return true
	})
}

func (da *DestinationAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context) {
	vs := r.Item.(*v1alpha3.VirtualService)

	routeProtocols := []string{"http", "tcp", "tls"}
	destinations := make([]*v1alpha3.Destination, 0)
	for _, protocol := range routeProtocols {
		destinations = append(destinations, getRouteDestinationsForProtocol(vs, protocol)...)
	}

	for _, destination := range destinations {
		if !da.checkDestination(r, destination.GetHost(), ctx) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.NotYetImplemented(r, fmt.Sprintf("TODO: virtualservices.nohost.hostnotfound %s", destination.GetHost())))
		}
	}

}

func (da *DestinationAnalyzer) checkDestination(r *resource.Entry, destHost string, ctx analysis.Context) bool {
	// https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#Destination
	// We need to handle two possible formats: short name (assume the nsame namespace as the rule) and FQDN
	// Need to check services from both platform registry + ServiceEntries
	//TODO

	// destName := resource.NewFullName // TODO: Handle FQDN

	fmt.Println("DEBUG0c-----------------------")
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, func(r *resource.Entry) bool {
		fmt.Println("DEBUG0a:", r.Metadata.Name)
		return true
	})
	ctx.ForEach(metadata.K8SCoreV1Services, func(r *resource.Entry) bool {
		fmt.Println("DEBUG0b:", r.Metadata.Name)
		return true
	})

	//TODO: Also check subsets?

	//TODO: How to make sure this is platform independent? Or at least the k8s stuff is marked as such?

	return true
}

//TODO: How to elegantly accomplish this in Go?
func getRouteDestinationsForProtocol(vs *v1alpha3.VirtualService, protocol string) []*v1alpha3.Destination {
	destinations := make([]*v1alpha3.Destination, 0)

	switch protocol {
	case "tcp":
		for _, r := range vs.GetTcp() {
			for _, rd := range r.GetRoute() {
				destinations = append(destinations, rd.GetDestination())
			}
		}
	case "tls":
		for _, r := range vs.GetTls() {
			for _, rd := range r.GetRoute() {
				destinations = append(destinations, rd.GetDestination())
			}
		}
	case "http":
		for _, r := range vs.GetHttp() {
			for _, rd := range r.GetRoute() {
				destinations = append(destinations, rd.GetDestination())
			}
		}
	}

	return destinations
}
