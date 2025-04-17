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

package ingress

import (
	"cmp"
	"errors"
	"fmt"
	"sort"
	"strings"

	knetworking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

const (
	IstioIngressController = "istio.io/ingress-controller"
)

var errNotFound = errors.New("item not found")

func VirtualServices(
	ruleHostIndex krt.Index[string, *IngressRule],
	services krt.Collection[ServiceWithPorts],
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[config.Config] {
	return krt.NewCollection(
		ruleHostIndex.AsCollection(),
		func(ctx krt.HandlerContext, hostRules krt.IndexObject[string, *IngressRule]) *config.Config {
			host, rules := hostRules.Key, hostRules.Objects
			if len(rules) == 0 {
				return nil
			}

			sortRulesByCreationTimestamp(rules)

			httpRoutes := make([]*networking.HTTPRoute, 0)
			for _, rule := range rules {
				httpRoutes = append(
					httpRoutes,
					convertIngressRule(rule.IngressName, rule.IngressNamespace, domainSuffix, rule.Rule, ctx, services)...,
				)
			}

			// sort routes to meet ingress route precedence requirements
			// see https://kubernetes.io/docs/concepts/services-networking/ingress/#multiple-matches
			sortHTTPRoutes(httpRoutes)

			virtualService := &networking.VirtualService{
				Hosts:    []string{host},
				Gateways: []string{fmt.Sprintf("%s/%s-%s-%s", IngressNamespace, rules[0].IngressName, constants.IstioIngressGatewayName, rules[0].IngressNamespace)},
				Http:     httpRoutes,
			}

			return &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             strings.Replace(host, ".", "-", -1) + "-" + rules[0].IngressName + "-" + constants.IstioIngressGatewayName,
					Namespace:        rules[0].IngressNamespace,
					Domain:           domainSuffix,
					Annotations:      map[string]string{constants.InternalRouteSemantics: constants.RouteSemanticsIngress},
				},
				Spec: virtualService,
			}
		},
		opts.WithName("DerivedVirtualServices")...,
	)
}

func sortHTTPRoutes(httpRoutes []*networking.HTTPRoute) {
	sort.SliceStable(httpRoutes, func(i, j int) bool {
		var r1Len, r2Len int
		var r1Ex, r2Ex bool
		if httpRoutes[i].Match != nil || len(httpRoutes[i].Match) != 0 {
			r1Len, r1Ex = getMatchURILength(httpRoutes[i].Match[0])
		}
		if httpRoutes[j].Match != nil || len(httpRoutes[j].Match) != 0 {
			r2Len, r2Ex = getMatchURILength(httpRoutes[j].Match[0])
		}
		// TODO: default at the end
		if r1Len == r2Len {
			return r1Ex && !r2Ex
		}
		return r1Len > r2Len
	})
}

func sortRulesByCreationTimestamp(rules []*IngressRule) {
	slices.SortFunc(rules, func(i, j *IngressRule) int {
		if r := i.CreationTimestamp.Compare(j.CreationTimestamp); r != 0 {
			return r
		}
		// If creation time is the same, then behavior is nondeterministic. In this case, we can
		// pick an arbitrary but consistent ordering based on name, namespace and rule index, which is unique.
		// CreationTimestamp is stored in seconds, so this is not uncommon.
		if r := cmp.Compare(i.IngressName, j.IngressName); r != 0 {
			return r
		}
		if r := cmp.Compare(i.IngressNamespace, j.IngressNamespace); r != 0 {
			return r
		}
		return cmp.Compare(i.RuleIndex, j.RuleIndex)
	})
}

func convertIngressRule(
	ingressName string,
	ingressNamespace string,
	domainSuffix string,
	rule *knetworking.IngressRule,
	ctx krt.HandlerContext,
	services krt.Collection[ServiceWithPorts],
) []*networking.HTTPRoute {
	httpRoutes := make([]*networking.HTTPRoute, 0, len(rule.HTTP.Paths))
	for _, httpPath := range rule.HTTP.Paths {
		httpMatch := &networking.HTTPMatchRequest{}
		if httpPath.PathType != nil {
			switch *httpPath.PathType {
			case knetworking.PathTypeExact:
				httpMatch.Uri = &networking.StringMatch{
					MatchType: &networking.StringMatch_Exact{Exact: httpPath.Path},
				}
			case knetworking.PathTypePrefix:
				// Optimize common case of / to not needed regex
				httpMatch.Uri = &networking.StringMatch{
					MatchType: &networking.StringMatch_Prefix{Prefix: httpPath.Path},
				}
			default:
				// Fallback to the legacy string matching
				// If the httpPath.Path is a wildcard path, Uri will be nil
				httpMatch.Uri = createFallbackStringMatch(httpPath.Path)
			}
		} else {
			httpMatch.Uri = createFallbackStringMatch(httpPath.Path)
		}

		httpRoute := ingressBackendToHTTPRoute(&httpPath.Backend, ingressNamespace, domainSuffix, ctx, services)
		if httpRoute == nil {
			log.Infof("invalid ingress rule %s:%s for host %q, no backend defined for path", ingressNamespace, ingressName, rule.Host)
			continue
		}
		// Only create a match if Uri is not nil. HttpMatchRequest cannot be empty
		if httpMatch.Uri != nil {
			httpRoute.Match = []*networking.HTTPMatchRequest{httpMatch}
		}
		httpRoutes = append(httpRoutes, httpRoute)
	}

	return httpRoutes
}

// getMatchURILength returns the length of matching path, and whether the match type is EXACT
func getMatchURILength(match *networking.HTTPMatchRequest) (length int, exact bool) {
	uri := match.GetUri()
	switch uri.GetMatchType().(type) {
	case *networking.StringMatch_Exact:
		return len(uri.GetExact()), true
	case *networking.StringMatch_Prefix:
		return len(uri.GetPrefix()), false
	}
	// should not happen
	return -1, false
}

func ingressBackendToHTTPRoute(
	backend *knetworking.IngressBackend,
	namespace string,
	domainSuffix string,
	ctx krt.HandlerContext,
	services krt.Collection[ServiceWithPorts],
) *networking.HTTPRoute {
	if backend == nil {
		return nil
	}

	port := &networking.PortSelector{}

	if backend.Service == nil {
		log.Infof("backend service must be specified")
		return nil
	}
	if backend.Service.Port.Number > 0 {
		port.Number = uint32(backend.Service.Port.Number)
	} else {
		resolvedPort, err := resolveNamedPort(backend, namespace, ctx, services)
		if err != nil {
			log.Infof("failed to resolve named port %s, error: %v", backend.Service.Port.Name, err)
			return nil
		}
		port.Number = uint32(resolvedPort)
	}

	return &networking.HTTPRoute{
		Route: []*networking.HTTPRouteDestination{
			{
				Destination: &networking.Destination{
					Host: fmt.Sprintf("%s.%s.svc.%s", backend.Service.Name, namespace, domainSuffix),
					Port: port,
				},
				Weight: 100,
			},
		},
	}
}

func resolveNamedPort(
	backend *knetworking.IngressBackend,
	namespace string,
	ctx krt.HandlerContext,
	services krt.Collection[ServiceWithPorts],
) (int32, error) {
	key := types.NamespacedName{Namespace: namespace, Name: backend.Service.Name}
	svc := krt.FetchOne(ctx, services, krt.FilterObjectName(key))
	if svc == nil {
		return 0, errNotFound
	}
	for _, port := range svc.Ports {
		if port.Name == backend.Service.Port.Name {
			return port.Port, nil
		}
	}
	return 0, errNotFound
}

// shouldProcessIngress determines whether the given Ingress resource should be processed
// by the controller, based on its Ingress class annotation
// (see https://kubernetes.io/docs/concepts/services-networking/ingress/#deprecated-annotation)
// or, in more recent versions of kubernetes (v1.18+), based on the Ingress's specified IngressClass
// (see https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-class)
func shouldProcessIngressWithClass(mesh *meshconfig.MeshConfig, ingress *knetworking.Ingress, ingressClass *knetworking.IngressClass) bool {
	if class, exists := ingress.Annotations[annotation.IoKubernetesIngressClass.Name]; exists {
		switch mesh.IngressControllerMode {
		case meshconfig.MeshConfig_OFF:
			return false
		case meshconfig.MeshConfig_STRICT:
			return class == mesh.IngressClass
		case meshconfig.MeshConfig_DEFAULT:
			return class == mesh.IngressClass
		default:
			log.Warnf("invalid ingress synchronization mode: %v", mesh.IngressControllerMode)
			return false
		}
	} else if ingressClass != nil {
		return ingressClass.Spec.Controller == IstioIngressController
	} else {
		switch mesh.IngressControllerMode {
		case meshconfig.MeshConfig_OFF:
			return false
		case meshconfig.MeshConfig_STRICT:
			return false
		case meshconfig.MeshConfig_DEFAULT:
			return true
		default:
			log.Warnf("invalid ingress synchronization mode: %v", mesh.IngressControllerMode)
			return false
		}
	}
}

func createFallbackStringMatch(s string) *networking.StringMatch {
	// If the string is empty or a wildcard, return nil
	if s == "" || s == "*" || s == "/*" || s == ".*" {
		return nil
	}

	// Note that this implementation only converts prefix and exact matches, not regexps.

	// Replace e.g. "foo.*" with prefix match
	if strings.HasSuffix(s, ".*") {
		return &networking.StringMatch{
			MatchType: &networking.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, ".*")},
		}
	}
	if strings.HasSuffix(s, "/*") {
		return &networking.StringMatch{
			MatchType: &networking.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "/*")},
		}
	}

	// Replace e.g. "foo" with a exact match
	return &networking.StringMatch{
		MatchType: &networking.StringMatch_Exact{Exact: s},
	}
}
