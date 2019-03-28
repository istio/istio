//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package conversions

import (
	"fmt"
	"path"
	"strings"

	"github.com/gogo/protobuf/proto"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"

	ingress "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var scope = log.RegisterScope("conversions", "proto converters for runtime state", 0)

// ToIngressSpec unwraps an MCP resource proto
func ToIngressSpec(e *mcp.Resource) (*ingress.IngressSpec, error) {

	p := metadata.K8sExtensionsV1beta1Ingresses.NewProtoInstance()
	i, ok := p.(*ingress.IngressSpec)
	if !ok {
		// Shouldn't happen
		return nil, fmt.Errorf("unable to convert proto to Ingress: %v", p)
	}

	if err := proto.Unmarshal(e.Body.Value, p); err != nil {
		// Shouldn't happen
		return nil, fmt.Errorf("unable to unmarshal Ingress during projection: %v", err)
	}

	return i, nil
}

// IngressToVirtualService converts from ingress spec to Istio VirtualServices
func IngressToVirtualService(key resource.VersionedKey, meta resource.Metadata, i *ingress.IngressSpec,
	domainSuffix string, ingressByHost map[string]resource.Entry) {
	// Ingress allows a single host - if missing '*' is assumed
	// We need to merge all rules with a particular host across
	// all ingresses, and return a separate VirtualService for each
	// host.

	namespace, name := key.FullName.InterpretAsNamespaceAndName()
	for _, rule := range i.Rules {
		if rule.HTTP == nil {
			scope.Infof("invalid ingress rule %s:%s for host %q, no paths defined", namespace, name, rule.Host)
			continue
		}

		host := rule.Host
		namePrefix := strings.Replace(host, ".", "-", -1)
		if host == "" {
			host = "*"
		}
		virtualService := &v1alpha3.VirtualService{
			Hosts:    []string{host},
			Gateways: []string{model.IstioIngressGatewayName},
		}

		httpRoutes := []*v1alpha3.HTTPRoute{}
		for _, path := range rule.HTTP.Paths {
			httpMatch := &v1alpha3.HTTPMatchRequest{
				Uri: createStringMatch(path.Path),
			}

			httpRoute := ingressBackendToHTTPRoute(&path.Backend, namespace, domainSuffix)
			if httpRoute == nil {
				scope.Infof("invalid ingress rule %s:%s for host %q, no backend defined for path", namespace, name, rule.Host)
				continue
			}
			httpRoute.Match = []*v1alpha3.HTTPMatchRequest{httpMatch}
			httpRoutes = append(httpRoutes, httpRoute)
		}

		virtualService.Http = httpRoutes

		newName := namePrefix + "-" + name + "-" + model.IstioIngressGatewayName
		newNamespace := model.IstioIngressNamespace

		old, f := ingressByHost[host]
		if f {
			vs := old.Item.(*v1alpha3.VirtualService)
			vs.Http = append(vs.Http, httpRoutes...)
		} else {
			ingressByHost[host] = resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						FullName:   resource.FullNameFromNamespaceAndName(newNamespace, newName),
						Collection: metadata.IstioNetworkingV1alpha3Virtualservices.Collection,
					},
					Version: key.Version,
				},
				Metadata: meta,
				Item:     virtualService,
			}
		}
	}

	// Matches * and "/". Currently not supported - would conflict
	// with any other explicit VirtualService.
	if i.Backend != nil {
		scope.Infof("Ignore default wildcard ingress, use VirtualService %s:%s",
			namespace, name)
	}
}

func createStringMatch(s string) *v1alpha3.StringMatch {
	if s == "" {
		return nil
	}

	// Note that this implementation only converts prefix and exact matches, not regexps.

	// Replace e.g. "foo.*" with prefix match
	if strings.HasSuffix(s, ".*") {
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, ".*")},
		}
	}
	if strings.HasSuffix(s, "/*") {
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "/*")},
		}
	}

	// Replace e.g. "foo" with a exact match
	return &v1alpha3.StringMatch{
		MatchType: &v1alpha3.StringMatch_Exact{Exact: s},
	}
}

func ingressBackendToHTTPRoute(backend *ingress.IngressBackend, namespace string, domainSuffix string) *v1alpha3.HTTPRoute {
	if backend == nil {
		return nil
	}

	port := &v1alpha3.PortSelector{
		Port: nil,
	}

	if backend.ServicePort.Type == intstr.Int {
		port.Port = &v1alpha3.PortSelector_Number{
			Number: uint32(backend.ServicePort.IntVal),
		}
	} else {
		// Port names are not allowed in destination rules.
		return nil
	}

	return &v1alpha3.HTTPRoute{
		Route: []*v1alpha3.HTTPRouteDestination{
			{
				Destination: &v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.%s", backend.ServiceName, namespace, domainSuffix),
					Port: port,
				},
				Weight: 100,
			},
		},
	}
}

// IngressToGateway converts from ingress spec to Istio Gateway
func IngressToGateway(key resource.VersionedKey, meta resource.Metadata, i *ingress.IngressSpec) resource.Entry {
	namespace, name := key.FullName.InterpretAsNamespaceAndName()

	gateway := &v1alpha3.Gateway{
		Selector: model.IstioIngressWorkloadLabels,
	}

	// FIXME this is a temporary hack until all test templates are updated
	//for _, tls := range i.Spec.TLS {
	if len(i.TLS) > 0 {
		tls := i.TLS[0] // FIXME
		// TODO validation when multiple wildcard tls secrets are given
		if len(tls.Hosts) == 0 {
			tls.Hosts = []string{"*"}
		}
		gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
			Port: &v1alpha3.Port{
				Number:   443,
				Protocol: string(model.ProtocolHTTPS),
				Name:     fmt.Sprintf("https-443-i-%s-%s", name, namespace),
			},
			Hosts: tls.Hosts,
			// While we accept multiple certs, we expect them to be mounted in
			// /etc/certs/namespace/secretname/tls.crt|tls.key
			Tls: &v1alpha3.Server_TLSOptions{
				HttpsRedirect: false,
				Mode:          v1alpha3.Server_TLSOptions_SIMPLE,
				// TODO this is no longer valid for the new v2 stuff
				PrivateKey:        path.Join(model.IngressCertsPath, model.IngressKeyFilename),
				ServerCertificate: path.Join(model.IngressCertsPath, model.IngressCertFilename),
				// TODO: make sure this is mounted
				CaCertificates: path.Join(model.IngressCertsPath, model.RootCertFilename),
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &v1alpha3.Server{
		Port: &v1alpha3.Port{
			Number:   80,
			Protocol: string(model.ProtocolHTTP),
			Name:     fmt.Sprintf("http-80-i-%s-%s", name, namespace),
		},
		Hosts: []string{"*"},
	})

	newName := name + "-" + model.IstioIngressGatewayName
	newNamespace := model.IstioIngressNamespace

	gw := resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName:   resource.FullNameFromNamespaceAndName(newNamespace, newName),
				Collection: metadata.IstioNetworkingV1alpha3Virtualservices.Collection,
			},
			Version: key.Version,
		},
		Metadata: meta,
		Item:     gateway,
	}

	return gw
}
