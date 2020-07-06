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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	listerv1 "k8s.io/client-go/listers/core/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	IstioIngressController = "istio.io/ingress-controller"
)

var (
	errNotFound = errors.New("item not found")
)

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

// defaultSelector defines the default selector that will be used if one is not provided
// This will select the default ingressgateway deployment provided by the standard installation
// Configurable by meshConfig.ingressSelector.
var defaultSelector = labels.Instance{constants.IstioLabel: constants.IstioIngressLabelValue}

// ConvertIngressV1alpha3 converts from ingress spec to Istio Gateway
func ConvertIngressV1alpha3(ingress v1beta1.Ingress, mesh *meshconfig.MeshConfig, domainSuffix string) model.Config {
	gateway := &networking.Gateway{}
	// Setup the selector for the gateway
	if len(mesh.IngressSelector) > 0 {
		// If explicitly defined, use this one
		gateway.Selector = labels.Instance{constants.IstioLabel: mesh.IngressSelector}
	} else if mesh.IngressService != "istio-ingressgateway" {
		// Otherwise, we will use the ingress service as the default. It is common for the selector and service
		// to be the same, so this removes the need for two configurations
		// However, if its istio-ingressgateway we need to use the old values for backwards compatibility
		gateway.Selector = labels.Instance{constants.IstioLabel: mesh.IngressService}
	} else {
		gateway.Selector = defaultSelector
	}

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

	gatewayConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: gvk.Gateway,
			Name:             ingress.Name + "-" + constants.IstioIngressGatewayName,
			Namespace:        ingressNamespace,
			Domain:           domainSuffix,
		},
		Spec: gateway,
	}

	return gatewayConfig
}

// ConvertIngressVirtualService converts from ingress spec to Istio VirtualServices
func ConvertIngressVirtualService(ingress v1beta1.Ingress, domainSuffix string, ingressByHost map[string]*model.Config, serviceLister listerv1.ServiceLister) {
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
			Hosts:    []string{},
			Gateways: []string{fmt.Sprintf("%s/%s-%s", ingressNamespace, ingress.Name, constants.IstioIngressGatewayName)},
		}

		virtualService.Hosts = []string{host}

		httpRoutes := make([]*networking.HTTPRoute, 0)
		for _, httpPath := range rule.HTTP.Paths {
			httpMatch := &networking.HTTPMatchRequest{}
			if httpPath.PathType != nil {
				switch *httpPath.PathType {
				case v1beta1.PathTypeExact:
					httpMatch.Uri = &networking.StringMatch{
						MatchType: &networking.StringMatch_Exact{Exact: httpPath.Path},
					}
				case v1beta1.PathTypePrefix:
					// From the spec: /foo/bar matches /foo/bar/baz, but does not match /foo/barbaz
					// Envoy prefix match behaves differently, so insert a / if we don't have one
					path := httpPath.Path
					if !strings.HasSuffix(path, "/") {
						path += "/"
					}
					httpMatch.Uri = &networking.StringMatch{
						MatchType: &networking.StringMatch_Prefix{Prefix: path},
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

		virtualServiceConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: gvk.VirtualService,
				Name:             namePrefix + "-" + ingress.Name + "-" + constants.IstioIngressGatewayName,
				Namespace:        ingress.Namespace,
				Domain:           domainSuffix,
			},
			Spec: virtualService,
		}

		old, f := ingressByHost[host]
		if f {
			vs := old.Spec.(*networking.VirtualService)
			vs.Http = append(vs.Http, httpRoutes...)
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
		} else {
			ingressByHost[host] = &virtualServiceConfig
		}
	}

	// Matches * and "/". Currently not supported - would conflict
	// with any other explicit VirtualService.
	if ingress.Spec.Backend != nil {
		log.Infof("Ignore default wildcard ingress, use VirtualService %s:%s",
			ingress.Namespace, ingress.Name)
	}
}

func ingressBackendToHTTPRoute(backend *v1beta1.IngressBackend, namespace string, domainSuffix string,
	serviceLister listerv1.ServiceLister) *networking.HTTPRoute {
	if backend == nil {
		return nil
	}

	port := &networking.PortSelector{}

	if backend.ServicePort.Type == intstr.Int {
		port.Number = uint32(backend.ServicePort.IntVal)
	} else {
		resolvedPort, err := resolveNamedPort(backend, namespace, serviceLister)
		if err != nil {
			log.Infof("failed to resolve named port %s, error: %v", backend.ServicePort.StrVal, err)
			return nil
		}
		port.Number = uint32(resolvedPort)
	}

	return &networking.HTTPRoute{
		Route: []*networking.HTTPRouteDestination{
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

func resolveNamedPort(backend *v1beta1.IngressBackend, namespace string, serviceLister listerv1.ServiceLister) (int32, error) {
	svc, err := serviceLister.Services(namespace).Get(backend.ServiceName)
	if err != nil {
		return 0, err
	}
	for _, port := range svc.Spec.Ports {
		if port.Name == backend.ServicePort.StrVal {
			return port.Port, nil
		}
	}
	return 0, errNotFound
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngressWithClass(mesh *meshconfig.MeshConfig, ingress *v1beta1.Ingress, ingressClass *v1beta1.IngressClass) bool {
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
		// TODO support ingressclass.kubernetes.io/is-default-class annotation
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
