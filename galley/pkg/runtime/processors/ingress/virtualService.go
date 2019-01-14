//  Copyright 2019 Istio Authors
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

package ingress

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"istio.io/istio/galley/pkg/runtime/processing"
	ingress "k8s.io/api/extensions/v1beta1"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
)

type vsConverter struct {
	generation int64
	table    processing.Table
	config   *Config
	listener processing.ProjectionListener

	// track collections generation to detect any changes that should retrigger a rebuild of cached state
	lastCollectionGen int64
	ingressByHosts    map[string]*resource.Entry
}

var _ processing.Projection = &vsConverter{}
var _ processing.Handler = &vsConverter{}

// Handle implements processing.Handler
func (v *vsConverter) Handle(event resource.Event) {
	switch event.Kind {
	case resource.Added, resource.Updated:
		v.table.Set()
	case resource.Deleted:

	default:
		scope.Errorf("Unrecognized event received: %v", event.Kind)
	}
}

// Type implements processing.View
func (v *vsConverter) Type() resource.TypeURL {
	return metadata.VirtualService.TypeURL
}

// Generation implements processing.Projection
func (v *vsConverter) Generation() int64 {
	v.rebuild()
	return v.lastCollectionGen
}

// Get implements processing.Projection
func (v *vsConverter) Get() []*mcp.Resource {
	v.rebuild()

	result := make([]*mcp.Resource, 0, len(v.ingressByHosts))
	for _, e := range v.ingressByHosts {
		env, err := resource.ToMcpResource(*e)
		if err != nil {
			scope.Errorf("Unable to envelope virtual service resource: %v", err)
			continue
		}
		result = append(result, env)
	}

	return result
}

func (v *vsConverter) SetProjectionListener(l processing.ProjectionListener) {
	v.listener = l
}

// rebuild the internal state of the view
func (v *vsConverter) rebuild() {
	if v.table.Generation() == v.lastCollectionGen {
		// No need to rebuild
		return
	}

	// Order names for stable generation.
	var orderedNames []resource.FullName
	for _, name := range v.table.Names() {
		orderedNames = append(orderedNames, name)
	}
	sort.Slice(orderedNames, func(i, j int) bool {
		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
	})

	ingressByHost := make(map[string]*resource.Entry)
	for _, name := range orderedNames {
		entry := v.table.Item(name)
		i := entry.Body.(*ingress.IngressSpec) // TODO

		ToVirtualService(entry.ID, i, entry.Metadata, v.config.DomainSuffix, ingressByHost)
	}

	if v.ingressByHosts == nil || !reflect.DeepEqual(v.ingressByHosts, ingressByHost) {
		v.ingressByHosts = ingressByHost
		v.lastCollectionGen = v.table.Generation()
		v.generation++
	}
}

// ToVirtualService converts from ingress spec to Istio VirtualServices
func ToVirtualService(key resource.VersionedKey, i *ingress.IngressSpec, meta resource.Metadata, domainSuffix string, ingressByHost map[string]*resource.Entry) {
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

			old.ID.Version = resource.Version(fmt.Sprintf("%s-%s-%s", old.ID.Version, key.FullName, key.Version))
		} else {
			ingressByHost[host] = &resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						FullName: resource.FullNameFromNamespaceAndName(newNamespace, newName),
						TypeURL:  metadata.VirtualService.TypeURL,
					},
					Version: resource.Version(fmt.Sprintf("%s-%s", key.FullName, key.Version)),
				},
				Item:     virtualService,
				Metadata: meta,
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
