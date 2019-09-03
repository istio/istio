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
	// TODO: This is likely inadequate
	fqdnPattern = regexp.MustCompile("(.+)\\.(.+)\\.svc\\.cluster\\.local")
)

// DestinationAnalyzer checks the hosts associated with each virtual service
// https://istio.io/docs/reference/config/networking/v1alpha3/virtual-service/#Destination
// TODO: This all needs lots of cleanup and correctness checking
type DestinationAnalyzer struct {
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
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	routeProtocols := []string{"http", "tcp", "tls"}
	destinations := make([]*v1alpha3.Destination, 0)
	for _, protocol := range routeProtocols {
		destinations = append(destinations, getRouteDestinationsForProtocol(vs, protocol)...)
	}

	for _, destination := range destinations {
		if !da.checkDestinationHost(ns, destination, ctx) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.NotYetImplemented(r,
				fmt.Sprintf("TODO: virtualservices.nohost.hostnotfound %s", destination.GetHost())))
			continue
		}
		if !da.checkDestinationSubset(ns, destination, ctx) {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices, msg.NotYetImplemented(r,
				fmt.Sprintf("TODO: Could not find matching host + subset %s + %s", destination.GetHost(), destination.GetSubset())))
		}
	}

}

func (da *DestinationAnalyzer) checkDestinationHost(vsNamespace string, destination *v1alpha3.Destination, ctx analysis.Context) bool {
	// Figure out the resource.Name to look up from the provided host string
	name := getResourceNameFromHost(vsNamespace, destination.GetHost())

	// Check explicitly defined ServiceEntries
	// Check services from the platform service registry
	// TODO: How to make sure this is platform independent?
	// TODO: Look at synthetic service entries? But they're not (currently) in the snapshot... and according to Oz "don't have quiesce characteristics" and we "may get one-off issues"
	if ctx.Find(metadata.IstioNetworkingV1Alpha3Serviceentries, name) == nil && ctx.Find(metadata.K8SCoreV1Services, name) == nil {
		return false
	}
	return true
}

func (da *DestinationAnalyzer) checkDestinationSubset(vsNamespace string, destination *v1alpha3.Destination, ctx analysis.Context) bool {
	// Figure out the resource.Name to look up from the provided host string
	name := getResourceNameFromHost(vsNamespace, destination.GetHost())

	subset := destination.GetSubset()

	// if there's no subset specified, we're done
	if subset == "" {
		return true
	}

	// TODO: Is it worth the extra effort to avoid repeated looping here? Probably... need to generate a set of what DR hostname+subset combos exist
	found := false
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Destinationrules, func(r *resource.Entry) bool {
		dr := r.Item.(*v1alpha3.DestinationRule)
		drNamespace, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

		// Skip over destination rules that don't match on host
		if name != getResourceNameFromHost(drNamespace, dr.GetHost()) {
			return true
		}

		for _, ss := range dr.GetSubsets() {
			if ss.GetName() == subset {
				found = true
				return false // Stop iterating since we found a match
			}
		}
		return true
	})
	return found
}

// TODO: Is there already a lib I can use for this? Or, failing that, a better place for this to live?
func getResourceNameFromHost(defaultNamespace, host string) resource.Name {
	// We need to handle two possible formats: short name (assumes the same namespace as the rule) and FQDN

	// First, try to parse as FQDN
	namespace, name := getNamespaceAndNameFromFQDN(host)

	// TODO: Do we also need to handle "namespace/name" ?

	//Otherwise, treat this as a short name and use the assumed namespace
	if namespace == "" {
		namespace = defaultNamespace
		name = host
	}
	return resource.NewName(namespace, name)
}

// TODO: Is there already a lib I can use for this? Or, failing that, a better place for this to live?
func getNamespaceAndNameFromFQDN(fqdn string) (string, string) {
	result := fqdnPattern.FindAllStringSubmatch(fqdn, -1)
	if len(result) == 0 {
		return "", ""
	}
	return result[0][0], result[0][1]
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
