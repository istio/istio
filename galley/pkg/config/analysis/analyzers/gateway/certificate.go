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

package gateway

import (
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type CertificateAnalyzer struct{}

var _ analysis.Analyzer = &CertificateAnalyzer{}

func (*CertificateAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "gateway.CertificateAnalyzer",
		Description: "Checks a gateway certificate",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (gateway *CertificateAnalyzer) Analyze(context analysis.Context) {
	scopeGatewayToNamespace := getScopeGatewayToNamespace(context)

	context.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(resource *resource.Instance) bool {
		gateway.analyzeDuplicateCertificate(resource, context, scopeGatewayToNamespace)
		return true
	})
}

func (gateway *CertificateAnalyzer) analyzeDuplicateCertificate(currentResource *resource.Instance, context analysis.Context, scopeGatewayToNamespace bool) {
	currentGateway := currentResource.Message.(*v1alpha3.Gateway)
	currentGatewayFullName := currentResource.Metadata.FullName
	gateways := getGatewaysWithSelector(context, scopeGatewayToNamespace, currentGatewayFullName, currentGateway.Selector)

	for _, gatewayFullName := range gateways {
		// ignore matching the same exact gateway
		if currentGatewayFullName == gatewayFullName {
			continue
		}

		gatewayInstance := context.Find(collections.IstioNetworkingV1Alpha3Gateways.Name(), gatewayFullName)
		gateway := gatewayInstance.Message.(*v1alpha3.Gateway)
		for _, currentServer := range currentGateway.Servers {
			for _, server := range gateway.Servers {
				// make sure have TLS configuration
				if currentServer.Tls == nil || server.Tls == nil {
					continue
				}

				if haveSameCertificate(currentServer.Tls, server.Tls) {
					gatewayNames := []string{currentGatewayFullName.String(), gatewayFullName.String()}
					message := msg.NewGatewayDuplicateCertificate(currentResource, gatewayNames)

					if line, ok := util.ErrorLine(currentResource, util.MetadataName); ok {
						message.Line = line
					}

					context.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), message)
				}
			}
		}
	}
}

func haveSameCertificate(currentGatewayTLS, gatewayTLS *v1alpha3.ServerTLSSettings) bool {
	if currentGatewayTLS.CredentialName != "" && gatewayTLS.CredentialName != "" {
		return currentGatewayTLS.CredentialName == gatewayTLS.CredentialName
	}

	if currentGatewayTLS.CredentialName == "" && gatewayTLS.CredentialName == "" {
		if currentGatewayTLS.ServerCertificate != "" && gatewayTLS.ServerCertificate != "" {
			if currentGatewayTLS.ServerCertificate == gatewayTLS.ServerCertificate {
				if currentGatewayTLS.PrivateKey != "" && gatewayTLS.PrivateKey != "" {
					return currentGatewayTLS.PrivateKey == gatewayTLS.PrivateKey
				}
				return false
			}
		}
	}

	return false
}

func getScopeGatewayToNamespace(context analysis.Context) bool {
	// todo, get PILOT_SCOPE_GATEWAY_TO_NAMESPACE environment variable
	return false
}

// get all gateways that is superset of the selector
func getGatewaysWithSelector(c analysis.Context, gwScope bool, currentGWName resource.FullName, currentGWSelector map[string]string) []resource.FullName {
	var gateways []resource.FullName

	c.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(resource *resource.Instance) bool {
		// if scopeToNamespace true, ignore adding gateways from other namespace
		if gwScope {
			if currentGWName.Namespace != resource.Metadata.FullName.Namespace {
				return true
			}
		}

		// if current gateway selector is empty, match all gateway
		if len(currentGWSelector) == 0 {
			gateways = append(gateways, resource.Metadata.FullName)
			return true
		}

		gateway := resource.Message.(*v1alpha3.Gateway)
		// if current gateway selector is subset of other gateway selector
		// add other gateway
		if selectorSubset(currentGWSelector, gateway.Selector) {
			gateways = append(gateways, resource.Metadata.FullName)
		}

		return true
	})

	return gateways
}

func selectorSubset(selectorX, selectorY map[string]string) bool {
	var count int

	for keyX, valueX := range selectorX {
		for keyY, valueY := range selectorY {
			if keyX == keyY {
				// if have same key but different value
				// mean selectorX is not subset of selectorY
				if valueX != valueY {
					return false
				}
				// if key and value is same
				// increase the counting
				count++
			}
		}
	}

	// if total counting is not same with the length
	// of selectorX, selectorX is not subset of selectorY
	return count == len(selectorX)
}
