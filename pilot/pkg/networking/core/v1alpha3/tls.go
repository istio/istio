package v1alpha3

import (
	"net"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
)

type SNIRoute struct {
	ServerNames []string
	Host        model.Hostname
	Subset      string
	Port        int
}

type hostPortKey struct {
	Host model.Hostname
	Port int
}

func sourceMatchTLS(match *v1alpha3.TLSMatchAttributes,
	proxyLabels model.LabelsCollection, gatewayNames map[string]bool) bool {

	if match == nil {
		return true
	}

	// Trim by source labels or mesh gateway
	if len(match.Gateways) > 0 {
		for _, g := range match.Gateways {
			if gatewayNames[g] {
				return true
			}
		}
	} else if proxyLabels.IsSupersetOf(match.GetSourceLabels()) {
		return true
	}

	return false
}

func buildSNIRoutes(configs []model.Config, proxyLabels model.LabelsCollection, gateways map[string]bool) map[hostPortKey][]SNIRoute {
	sniRoutes := make(map[hostPortKey][]SNIRoute) // host+port match -> sni routes
	for _, config := range configs {
		vs := config.Spec.(*v1alpha3.VirtualService)
		for _, host := range vs.Hosts {
			if net.ParseIP(host) != nil {
				continue // ignore IP hosts
			}

			fqdn := model.ResolveShortnameToFQDN(host, config.ConfigMeta)
			for _, tls := range vs.Tls {
				// we don't support weighted routing yet, so there is only one destination
				dest := tls.Route[0].Destination

				// TODO: len(tls.Match) == 0?
				for _, match := range tls.Match {
					if !sourceMatchTLS(match, proxyLabels, gateways) {
						continue
					}

					// TODO: L4 match attributes
					key := hostPortKey{Host: fqdn, Port: int(match.Port)}
					sniRoutes[key] = append(sniRoutes[key], SNIRoute{
						ServerNames: match.SniHosts,
						Host:        model.Hostname(dest.Host),
						Port:        int(dest.Port.GetNumber()),
						Subset:      dest.Subset,
					})
				}
			}
		}
	}

	return sniRoutes
}
