package v1alpha3

import (
	"net"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
)

type SNIRoute struct {
	ServerNames []string
	Host        string
	Subset      string
	Port        int
}

type hostPortKey struct {
	Host string
	Port int
}

func sourceMatchTCP(match v1alpha3.L4MatchAttributes,
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

func buildSNIRoutes(configs []model.Config, services []*model.Service,
	proxyLabels model.LabelsCollection, gateways map[string]bool) map[hostPortKey][]SNIRoute {

	sniRoutes := make(map[hostPortKey][]SNIRoute) // host+port match -> sni routes

	isSNI := make(map[hostPortKey]bool)
	for _, service := range services {
		for _, port := range service.Ports {
			if port.Protocol.IsTLS() {
				key := hostPortKey{Host: service.Hostname, Port: port.Port}
				isSNI[key] = true
			}
		}
	}

	// build SNI routes from configs
	for _, config := range configs {
		vs := config.Spec.(*v1alpha3.VirtualService)
		for _, host := range vs.Hosts {
			if net.ParseIP(host) != nil {
				continue // ignore IP hosts
			}

			fqdn := model.ResolveShortnameToFQDN(host, config.ConfigMeta)
			for _, tcp := range vs.Tcp {
				// TODO: match port. this assumes the match port and destination port are the same.

				// we don't support weighted routing yet, so there is only one destination
				dest := tcp.Route[0].Destination
				key := hostPortKey{Host: fqdn, dest.Port}
				if isSNI[key] {
					if len(tcp.Match) == 0 {
						sniRoutes[key] = append(sniRoutes[key], SNIRoute{
							ServerNames: []string{fqdn},
							Host:        dest.Host,
							Port:        dest.Port,
							Subset:      dest.Subset,
						})
					} else {
						for _, match := range tcp.Match {
							if !sourceMatchTCP(match, proxyLabels, gateways) {
								continue
							}

							// TODO: L4 match attributes
							sniRoutes[key] = append(sniRoutes[key], SNIRoute{
								ServerNames: []string{fqdn},
								Host:        dest.Host,
								Port:        dest.Port,
								Subset:      dest.Subset,
							})
						}
					}

				}
			}
		}
	}

	// build default SNI routes for SNI services not handled by configs
	isHandled := make(map[model.Hostname]bool)
	for _, config := range configs {
		vs := config.Spec.(*v1alpha3.VirtualService)
		for _, host := range vs.Hosts {
			fqdn := model.ResolveShortnameToFQDN(host, config.ConfigMeta)
			isHandled[fqdn] = true
		}
	}
	for _, service := range services {
		if !isHandled[service.Hostname] {
			for _, port := range service.Ports {
				key := hostPortKey{Host: service.Hostname, Port: port.Port}
				if isSNI[key] {
					sniRoutes[key] = append(sniRoutes[key], SNIRoute{
						ServerNames: []string{service.Hostname},
						Host:        service.Hostname,
						Port:        port.Port,
					})
				}
			}
		}
	}

	return sniRoutes
}
