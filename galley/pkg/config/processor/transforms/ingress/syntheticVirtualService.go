// Copyright 2019 Istio Authors
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

package ingress

import (
	"sort"
	"strings"

	"k8s.io/api/extensions/v1beta1"

	"istio.io/api/annotation"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/synthesize"
)

// syntheticVirtualService represents an in-memory state that maps ingress resources to a synthesized Virtual Service.
type syntheticVirtualService struct {
	// host is the key for the mapping.
	host string

	// Keep track of resource name. Depending on the ingresses that participate, the name can change.
	name    resource.Name
	version resource.Version

	// ingresses that are represented in this Virtual Service
	ingresses []*resource.Entry
}

func (s *syntheticVirtualService) attachIngress(e *resource.Entry) (resource.Name, resource.Version) {
	var found bool
	for i, existing := range s.ingresses {
		if existing.Metadata.Name == e.Metadata.Name {
			s.ingresses[i] = e
			found = true
			break
		}
	}

	if !found {
		s.ingresses = append(s.ingresses, e)
	}

	sort.SliceStable(s.ingresses, func(i, j int) bool {
		return strings.Compare(s.ingresses[i].Metadata.Name.String(), s.ingresses[j].Metadata.Name.String()) < 0
	})

	oldName := s.name
	oldVersion := s.version

	s.name = generateSyntheticVirtualServiceName(s.host, s.ingresses[0].Metadata.Name)
	s.version = s.generateVersion()

	return oldName, oldVersion
}

func generateSyntheticVirtualServiceName(host string, ingressName resource.Name) resource.Name {
	_, name := ingressName.InterpretAsNamespaceAndName()

	namePrefix := strings.Replace(host, ".", "-", -1)

	newName := namePrefix + "-" + name + "-" + IstioIngressGatewayName
	newNamespace := IstioIngressNamespace

	return resource.NewName(newNamespace, newName)
}

func (s *syntheticVirtualService) detachIngress(e *resource.Entry) (resource.Name, resource.Version) {
	for i, existing := range s.ingresses {
		if existing.Metadata.Name == e.Metadata.Name {
			s.ingresses = append(s.ingresses[:i], s.ingresses[i+1:]...)
			oldName := s.name
			oldVersion := s.version

			if i == 0 {
				if len(s.ingresses) == 0 {
					s.name = resource.Name{}
					s.version = resource.Version("")
				} else {
					s.name = generateSyntheticVirtualServiceName(s.host, s.ingresses[0].Metadata.Name)
					s.version = s.generateVersion()
				}
			}
			return oldName, oldVersion
		}
	}

	return s.name, s.version
}

func (s *syntheticVirtualService) isEmpty() bool {
	return len(s.ingresses) == 0
}

func (s *syntheticVirtualService) generateEntry(domainSuffix string) *resource.Entry {
	// Ingress allows a single host - if missing '*' is assumed
	// We need to merge all rules with a particular host across
	// all ingresses, and return a separate VirtualService for each
	// host.

	first := s.ingresses[0]
	namespace, name := first.Metadata.Name.InterpretAsNamespaceAndName()

	meta := first.Metadata.Clone()
	meta.Name = s.name
	meta.Version = s.version
	if meta.Annotations != nil {
		delete(meta.Annotations, annotation.IoKubernetesIngressClass.Name)
	}

	virtualService := &v1alpha3.VirtualService{
		Hosts:    []string{s.host},
		Gateways: []string{IstioIngressGatewayName},
	}

	for _, ing := range s.ingresses {
		ingress := ing.Item.(*v1beta1.IngressSpec)
		for _, rule := range ingress.Rules {
			if rule.HTTP == nil {
				scope.Processing.Errorf("invalid ingress rule %s:%s for host %q, no paths defined", namespace, name, rule.Host)
				continue
			}

			var httpRoutes []*v1alpha3.HTTPRoute
			for _, path := range rule.HTTP.Paths {
				httpMatch := &v1alpha3.HTTPMatchRequest{
					Uri: createStringMatch(path.Path),
				}

				httpRoute := ingressBackendToHTTPRoute(&path.Backend, namespace, domainSuffix)
				if httpRoute == nil {
					scope.Processing.Errorf("invalid ingress rule %s:%s for host %q, no backend defined for path", namespace, name, rule.Host)
					continue
				}
				httpRoute.Match = []*v1alpha3.HTTPMatchRequest{httpMatch}
				httpRoutes = append(httpRoutes, httpRoute)
			}

			virtualService.Http = append(virtualService.Http, httpRoutes...)
		}

		// Matches * and "/". Currently not supported - would conflict
		// with any other explicit VirtualService.
		if ingress.Backend != nil {
			scope.Processing.Infof("Ignore default wildcard ingress, use VirtualService %s:%s",
				namespace, name)
		}
	}

	return &resource.Entry{
		Metadata: meta,
		Item:     virtualService,
		Origin:   first.Origin,
	}
}

func (s *syntheticVirtualService) generateVersion() resource.Version {
	i := 0
	return synthesize.VersionIter("ing", func() (n resource.Name, v resource.Version, ok bool) {
		if i < len(s.ingresses) {
			ing := s.ingresses[i]
			i++
			n = ing.Metadata.Name
			v = ing.Metadata.Version
			ok = true
		}
		return
	})
}
