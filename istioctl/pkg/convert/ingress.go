// Copyright 2018 Istio Authors.
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

package convert

import (
	"fmt"
	"os"
	"path"
	"strings"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

// IstioIngresses converts K8s extensions/v1beta1 Ingresses with Istio rules to v1alpha3 gateway and virtual service
func IstioIngresses(ingresses []*v1beta1.Ingress, domainSuffix string) ([]model.Config, error) {

	if len(ingresses) == 0 {
		return make([]model.Config, 0), nil
	}
	if len(domainSuffix) == 0 {
		domainSuffix = "cluster.local"
	}

	gateways := make([]model.Config, 0)
	virtualServices := make([]model.Config, 0)

	for _, ingrezz := range ingresses {
		gateway, virtualService := ConvertIngressV1alpha3(*ingrezz, domainSuffix)
		// Override the generated namespace; the supplied one is needed to resolve non-fully qualified hosts
		gateway.Namespace = ingrezz.Namespace
		virtualService.Namespace = ingrezz.Namespace
		gateways = append(gateways, gateway)
		virtualServices = append(virtualServices, virtualService)
	}

	merged := model.MergeGateways(gateways...)

	// Make a list of the servers.  Don't attempt any extra merging beyond MergeGateways() impl.
	allServers := make([]*networking.Server, 0)
	for _, servers := range merged.Servers {
		allServers = append(allServers, servers...)
	}

	// Convert the merged Gateway back into a model.Config
	mergedGateway := model.Config{
		ConfigMeta: gateways[0].ConfigMeta,
		Spec: &networking.Gateway{
			Servers:  allServers,
			Selector: map[string]string{"istio": "ingressgateway"},
		},
	}

	// Fix the name of the gateway
	mergedGateway.Name = model.IstioIngressGatewayName

	// Ensure the VirtualServices all point to mergedGateway
	for _, virtualService := range virtualServices {
		virtualService.Spec.(*networking.VirtualService).Gateways[0] = mergedGateway.ConfigMeta.Name
	}

	out := []model.Config{mergedGateway}
	out = append(out, virtualServices...)

	return out, nil
}

// convertIngressV1alpha3 (and the two functions below that it calls) could be replaced with
// the version in _pilot/pkg/kube/ingress/conversion.go_ if the generation of VirtualServices
// was restored.
func convertIngressV1alpha3(ingress v1beta1.Ingress, domainSuffix string) (model.Config, model.Config) {
	gateway := &networking.Gateway{
		Selector: model.IstioIngressWorkloadLabels,
	}

	if len(ingress.Spec.TLS) > 0 {
		tls := ingress.Spec.TLS[0] // FIXME
		// TODO validation when multiple wildcard tls secrets are given
		if len(tls.Hosts) == 0 {
			tls.Hosts = []string{"*"}
		}
		gateway.Servers = append(gateway.Servers, &networking.Server{
			Port: &networking.Port{
				Number:   443,
				Protocol: string(model.ProtocolHTTPS),
				Name:     fmt.Sprintf("https-443-ingress-%s-%s", ingress.Name, ingress.Namespace),
			},
			Hosts: tls.Hosts,
			// While we accept multiple certs, we expect them to be mounted in
			// /etc/istio/certs/namespace/secretname/tls.crt|tls.key
			Tls: &networking.Server_TLSOptions{
				HttpsRedirect: false,
				Mode:          networking.Server_TLSOptions_SIMPLE,
				// TODO this is no longer valid for the new v2 stuff
				PrivateKey:        path.Join(model.IngressCertsPath, model.IngressKeyFilename),
				ServerCertificate: path.Join(model.IngressCertsPath, model.IngressCertFilename),
				// TODO: make sure this is mounted
				CaCertificates: path.Join(model.IngressCertsPath, model.RootCertFilename),
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &networking.Server{
		Port: &networking.Port{
			Number:   80,
			Protocol: string(model.ProtocolHTTP),
			Name:     fmt.Sprintf("http-80-ingress-%s-%s", ingress.Name, ingress.Namespace),
		},
		Hosts: []string{"*"},
	})

	virtualService := &networking.VirtualService{
		Hosts:    []string{"*"},
		Gateways: []string{model.IstioIngressGatewayName},
	}

	var httpRoutes []*networking.HTTPRoute
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			fmt.Fprintf(os.Stderr, "invalid ingress rule for host %q, no paths defined", rule.Host)
			continue
		}

		for _, path := range rule.HTTP.Paths {
			httpMatch := &networking.HTTPMatchRequest{
				Uri:       createStringMatch(path.Path),
				Authority: createStringMatch(rule.Host),
			}

			httpRoute := ingressBackendToHTTPRoute(&path.Backend, ingress.Namespace, domainSuffix)
			if httpRoute == nil {
				fmt.Fprintf(os.Stderr, "invalid ingress rule for host %q, no backend defined for path", rule.Host)
				continue
			}
			httpRoute.Match = []*networking.HTTPMatchRequest{httpMatch}
			httpRoutes = append(httpRoutes, httpRoute)
		}
	}

	if ingress.Spec.Backend != nil {
		httpRoutes = append(httpRoutes, ingressBackendToHTTPRoute(ingress.Spec.Backend, ingress.Namespace, domainSuffix))
	}

	virtualService.Http = httpRoutes

	gatewayConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.Gateway.Type,
			Group:     model.Gateway.Group,
			Version:   model.Gateway.Version,
			Name:      ingress.Name + "-" + model.IstioIngressGatewayName,
			Namespace: model.IstioIngressNamespace,
			Domain:    domainSuffix,
		},
		Spec: gateway,
	}

	virtualServiceConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.VirtualService.Type,
			Group:     model.VirtualService.Group,
			Version:   model.VirtualService.Version,
			Name:      ingress.Name + "-" + model.IstioIngressGatewayName,
			Namespace: model.IstioIngressNamespace,
			Domain:    domainSuffix,
		},
		Spec: virtualService,
	}

	return gatewayConfig, virtualServiceConfig

}

func ingressBackendToHTTPRoute(backend *v1beta1.IngressBackend, namespace string, domainSuffix string) *networking.HTTPRoute {
	if backend == nil {
		return nil
	}

	port := &networking.PortSelector{
		Port: nil,
	}

	if backend.ServicePort.Type == intstr.Int {
		port.Port = &networking.PortSelector_Number{
			Number: uint32(backend.ServicePort.IntVal),
		}
	} else {
		// Port names are not allowed in destination rules.
		return nil
	}

	return &networking.HTTPRoute{
		Route: []*networking.DestinationWeight{
			{
				Destination: &networking.Destination{
					Host: fmt.Sprintf("%s.%s.svc.%s", backend.ServiceName, namespace, domainSuffix),
					Port: port,
				},
				Weight: 100,
			},
		},
	}
}

func createStringMatch(s string) *networking.StringMatch {
	if s == "" {
		return nil
	}

	// Note that this implementation only converts prefix and exact matches, not regexps.

	// Replace e.g. "foo.*" with prefix match
	if strings.HasSuffix(s, ".*") {
		return &networking.StringMatch{
			MatchType: &networking.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, ".*")},
		}
	}

	// Replace e.g. "foo" with a exact match
	return &networking.StringMatch{
		MatchType: &networking.StringMatch_Exact{Exact: s},
	}
}
