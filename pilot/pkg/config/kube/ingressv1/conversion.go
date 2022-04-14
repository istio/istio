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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	knetworking "k8s.io/api/networking/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/log"
)

const (
	IstioIngressController = "istio.io/ingress-controller"
)

var errNotFound = errors.New("item not found")

// EncodeIngressRuleName encodes an ingress rule name for a given ingress resource name,
// as well as the position of the rule and path specified within it, counting from 1.
// ruleNum == pathNum == 0 indicates the default backend specified for an ingress.
func EncodeIngressRuleName(ingressName string, ruleNum, pathNum int) string {
	return fmt.Sprintf("%s-%d-%d", ingressName, ruleNum, pathNum)
}

// decodeIngressRuleName decodes an ingress rule name previously encoded with EncodeIngressRuleName.
func decodeIngressRuleName(name string) (ingressName string, ruleNum, pathNum int, err error) {
	parts := strings.Split(name, "-")
	if len(parts) < 3 {
		err = fmt.Errorf("could not decode string into ingress rule name: %s", name)
		return
	}

	ingressName = strings.Join(parts[0:len(parts)-2], "-")
	ruleNum, ruleErr := strconv.Atoi(parts[len(parts)-2])
	pathNum, pathErr := strconv.Atoi(parts[len(parts)-1])

	if pathErr != nil || ruleErr != nil {
		err = multierror.Append(
			fmt.Errorf("could not decode string into ingress rule name: %s", name),
			pathErr, ruleErr)
		return
	}

	return
}

// ConvertIngressV1alpha3 converts from ingress spec to Istio Gateway
func ConvertIngressV1alpha3(ingress knetworking.Ingress, mesh *meshconfig.MeshConfig, domainSuffix string) config.Config {
	gateway := &networking.Gateway{}
	gateway.Selector = getIngressGatewaySelector(mesh.IngressSelector, mesh.IngressService)

	for i, tls := range ingress.Spec.TLS {
		if tls.SecretName == "" {
			log.Infof("invalid ingress rule %s:%s for hosts %q, no secretName defined", ingress.Namespace, ingress.Name, tls.Hosts)
			continue
		}
		// TODO validation when multiple wildcard tls secrets are given
		if len(tls.Hosts) == 0 {
			tls.Hosts = []string{"*"}
		}
		gateway.Servers = append(gateway.Servers, &networking.Server{
			Port: &networking.Port{
				Number:   443,
				Protocol: string(protocol.HTTPS),
				Name:     fmt.Sprintf("https-443-ingress-%s-%s-%d", ingress.Name, ingress.Namespace, i),
			},
			Hosts: tls.Hosts,
			Tls: &networking.ServerTLSSettings{
				HttpsRedirect:  false,
				Mode:           networking.ServerTLSSettings_SIMPLE,
				CredentialName: tls.SecretName,
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &networking.Server{
		Port: &networking.Port{
			Number:   80,
			Protocol: string(protocol.HTTP),
			Name:     fmt.Sprintf("http-80-ingress-%s-%s", ingress.Name, ingress.Namespace),
		},
		Hosts: []string{"*"},
	})

	gatewayConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.Gateway,
			Name:             ingress.Name + "-" + constants.IstioIngressGatewayName + "-" + ingress.Namespace,
			Namespace:        ingressNamespace,
			Domain:           domainSuffix,
		},
		Spec: gateway,
	}

	return gatewayConfig
}

// ConvertIngressVirtualService converts from ingress spec to Istio VirtualServices
func ConvertIngressVirtualService(ingress knetworking.Ingress, domainSuffix string,
	ingressByHost map[string]*config.Config, serviceLister listerv1.ServiceLister) {
	// Ingress allows a single host - if missing '*' is assumed
	// We need to merge all rules with a particular host across
	// all ingresses, and return a separate VirtualService for each
	// host.
	if ingressNamespace == "" {
		ingressNamespace = constants.IstioIngressNamespace
	}

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			log.Infof("invalid ingress rule %s:%s for host %q, no paths defined", ingress.Namespace, ingress.Name, rule.Host)
			continue
		}

		host := rule.Host
		namePrefix := strings.Replace(host, ".", "-", -1)
		if host == "" {
			host = "*"
		}
		virtualService := &networking.VirtualService{
			Hosts:    []string{host},
			Gateways: []string{fmt.Sprintf("%s/%s-%s-%s", ingressNamespace, ingress.Name, constants.IstioIngressGatewayName, ingress.Namespace)},
		}

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
					httpMatch.Uri = createFallbackStringMatch(httpPath.Path)
				}
			} else {
				httpMatch.Uri = createFallbackStringMatch(httpPath.Path)
			}

			httpRoute := ingressBackendToHTTPRoute(&httpPath.Backend, ingress.Namespace, domainSuffix, serviceLister)
			if httpRoute == nil {
				log.Infof("invalid ingress rule %s:%s for host %q, no backend defined for path", ingress.Namespace, ingress.Name, rule.Host)
				continue
			}
			httpRoute.Match = []*networking.HTTPMatchRequest{httpMatch}
			httpRoutes = append(httpRoutes, httpRoute)
		}

		virtualService.Http = httpRoutes

		virtualServiceConfig := config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.VirtualService,
				Name:             namePrefix + "-" + ingress.Name + "-" + constants.IstioIngressGatewayName,
				Namespace:        ingress.Namespace,
				Domain:           domainSuffix,
				Annotations:      map[string]string{constants.InternalRouteSemantics: constants.RouteSemanticsIngress},
			},
			Spec: virtualService,
		}

		old, f := ingressByHost[host]
		if f {
			vs := old.Spec.(*networking.VirtualService)
			vs.Http = append(vs.Http, httpRoutes...)
			if features.LegacyIngressBehavior {
				sort.SliceStable(vs.Http, func(i, j int) bool {
					r1 := vs.Http[i].Match[0].GetUri()
					r2 := vs.Http[j].Match[0].GetUri()
					_, r1Ex := r1.GetMatchType().(*networking.StringMatch_Exact)
					_, r2Ex := r2.GetMatchType().(*networking.StringMatch_Exact)
					// TODO: default at the end
					if r1Ex && !r2Ex {
						return true
					}
					return false
				})
			}
		} else {
			ingressByHost[host] = &virtualServiceConfig
		}

		if !features.LegacyIngressBehavior {
			// sort routes to meet ingress route precedence requirements
			// see https://kubernetes.io/docs/concepts/services-networking/ingress/#multiple-matches
			vs := ingressByHost[host].Spec.(*networking.VirtualService)
			sort.SliceStable(vs.Http, func(i, j int) bool {
				r1Len, r1Ex := getMatchURILength(vs.Http[i].Match[0])
				r2Len, r2Ex := getMatchURILength(vs.Http[j].Match[0])
				// TODO: default at the end
				if r1Len == r2Len {
					return r1Ex && !r2Ex
				}
				return r1Len > r2Len
			})
		}
	}

	// Matches * and "/". Currently not supported - would conflict
	// with any other explicit VirtualService.
	if ingress.Spec.DefaultBackend != nil {
		log.Infof("Ignore default wildcard ingress, use VirtualService %s:%s",
			ingress.Namespace, ingress.Name)
	}
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

func ingressBackendToHTTPRoute(backend *knetworking.IngressBackend, namespace string, domainSuffix string,
	serviceLister listerv1.ServiceLister) *networking.HTTPRoute {
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
		resolvedPort, err := resolveNamedPort(backend, namespace, serviceLister)
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

func resolveNamedPort(backend *knetworking.IngressBackend, namespace string, serviceLister listerv1.ServiceLister) (int32, error) {
	svc, err := serviceLister.Services(namespace).Get(backend.Service.Name)
	if err != nil {
		return 0, err
	}
	for _, port := range svc.Spec.Ports {
		if port.Name == backend.Service.Port.Name {
			return port.Port, nil
		}
	}
	return 0, errNotFound
}

// shouldProcessIngress determines whether the given knetworking resource should be processed
// by the controller, based on its knetworking class annotation or, in more recent versions of
// kubernetes (v1.18+), based on the Ingress's specified IngressClass
// See https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-class
func shouldProcessIngressWithClass(mesh *meshconfig.MeshConfig, ingress *knetworking.Ingress, ingressClass *knetworking.IngressClass) bool {
	if class, exists := ingress.Annotations[kube.IngressClassAnnotation]; exists {
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

func getIngressGatewaySelector(ingressSelector, ingressService string) map[string]string {
	// Setup the selector for the gateway
	if ingressSelector != "" {
		// If explicitly defined, use this one
		return labels.Instance{constants.IstioLabel: ingressSelector}
	} else if ingressService != "istio-ingressgateway" && ingressService != "" {
		// Otherwise, we will use the ingress service as the default. It is common for the selector and service
		// to be the same, so this removes the need for two configurations
		// However, if its istio-ingressgateway we need to use the old values for backwards compatibility
		return labels.Instance{constants.IstioLabel: ingressService}
	} else {
		// If we have neither an explicitly defined ingressSelector or ingressService then use a selector
		// pointing to the ingressgateway from the default installation
		return labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue}
	}
}
