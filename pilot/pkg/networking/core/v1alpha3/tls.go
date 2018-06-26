// Copyright 2017 Istio Authors
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

package v1alpha3

import (
	"net"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

type sniRoute struct {
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

func buildSNIRoutes(configs []model.Config, proxyLabels model.LabelsCollection, gateways map[string]bool) map[hostPortKey][]sniRoute {
	sniRoutes := make(map[hostPortKey][]sniRoute) // host+port match -> sni routes
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
				for _, match := range tls.Match {
					if !sourceMatchTLS(match, proxyLabels, gateways) {
						continue
					}

					// TODO: L4 match attributes
					key := hostPortKey{Host: fqdn, Port: int(match.Port)}
					sniRoutes[key] = append(sniRoutes[key], sniRoute{
						ServerNames: match.SniHosts, // FIXME: can be empty!
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
