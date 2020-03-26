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

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/synthesize"
	"istio.io/istio/pkg/config/resource"
)

// syntheticVirtualService represents an in-memory state that maps ingress resources to a synthesized Virtual Service.
type syntheticVirtualService struct {
	// host is the key for the mapping.
	host string

	// Keep track of resource name. Depending on the ingresses that participate, the name can change.
	name    resource.FullName
	version resource.Version

	// ingresses that are represented in this Virtual Service
	ingresses []*resource.Instance
}

func (s *syntheticVirtualService) attachIngress(r *resource.Instance) (resource.FullName, resource.Version) {
	var found bool
	for i, existing := range s.ingresses {
		if existing.Metadata.FullName == r.Metadata.FullName {
			s.ingresses[i] = r
			found = true
			break
		}
	}

	if !found {
		s.ingresses = append(s.ingresses, r)
	}

	sort.SliceStable(s.ingresses, func(i, j int) bool {
		return strings.Compare(s.ingresses[i].Metadata.FullName.String(), s.ingresses[j].Metadata.FullName.String()) < 0
	})

	oldName := s.name
	oldVersion := s.version

	s.name = generateSyntheticVirtualServiceName(s.host, s.ingresses[0].Metadata.FullName)
	s.version = s.generateVersion()

	return oldName, oldVersion
}

func generateSyntheticVirtualServiceName(host string, name resource.FullName) resource.FullName {
	namePrefix := strings.Replace(host, ".", "-", -1)

	name.Name = resource.LocalName(namePrefix + "-" + string(name.Name) + "-" + IstioIngressGatewayName)
	name.Namespace = IstioIngressNamespace

	return name
}

func (s *syntheticVirtualService) detachIngress(r *resource.Instance) (resource.FullName, resource.Version) {
	for i, existing := range s.ingresses {
		if existing.Metadata.FullName == r.Metadata.FullName {
			s.ingresses = append(s.ingresses[:i], s.ingresses[i+1:]...)
			oldName := s.name
			oldVersion := s.version

			if i == 0 {
				if len(s.ingresses) == 0 {
					s.name = resource.FullName{}
					s.version = ""
				} else {
					s.name = generateSyntheticVirtualServiceName(s.host, s.ingresses[0].Metadata.FullName)
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

func (s *syntheticVirtualService) generateEntry(domainSuffix string) *resource.Instance {
	// Ingress allows a single host - if missing '*' is assumed
	// We need to merge all rules with a particular host across
	// all ingresses, and return a separate VirtualService for each
	// host.

	first := s.ingresses[0]

	namespace := first.Metadata.FullName.Namespace
	name := first.Metadata.FullName.Name

	meta := first.Metadata.Clone()
	meta.FullName = s.name
	meta.Version = s.version
	if meta.Annotations != nil {
		delete(meta.Annotations, annotation.IoKubernetesIngressClass.Name)
	}

	virtualService := &v1alpha3.VirtualService{
		Hosts:    []string{s.host},
		Gateways: []string{IstioIngressGatewayName},
	}

	for _, ing := range s.ingresses {
		ingress := ing.Message.(*v1beta1.IngressSpec)
		for _, rule := range ingress.Rules {
			if rule.HTTP == nil {
				scope.Processing.Errorf("invalid ingress rule %s:%s for host %q, no paths defined", namespace, name, rule.Host)
				continue
			}

			// Skip over any rules that are specific to a host that don't match the current synthetic virtual service host
			if rule.Host != "" && rule.Host != s.host {
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

	return &resource.Instance{
		Metadata: meta,
		Message:  virtualService,
		Origin:   first.Origin,
	}
}

func (s *syntheticVirtualService) generateVersion() resource.Version {
	i := 0
	return synthesize.VersionIter("ing", func() (n resource.FullName, v resource.Version, ok bool) {
		if i < len(s.ingresses) {
			ing := s.ingresses[i]
			i++
			n = ing.Metadata.FullName
			v = ing.Metadata.Version
			ok = true
		}
		return
	})
}
