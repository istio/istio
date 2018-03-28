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

package ingress

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

func convertIngress(ingress v1beta1.Ingress, domainSuffix string) []model.Config {
	out := make([]model.Config, 0)
	tls := ""

	if len(ingress.Spec.TLS) > 0 {
		// TODO(istio/istio/issues/1424): implement SNI
		if len(ingress.Spec.TLS) > 1 {
			log.Warnf("ingress %s requires several TLS secrets but Envoy can only serve one", ingress.Name)
		}
		secret := ingress.Spec.TLS[0]
		tls = fmt.Sprintf("%s.%s", secret.SecretName, ingress.Namespace)
	}

	if ingress.Spec.Backend != nil {
		name := EncodeIngressRuleName(ingress.Name, 0, 0)
		ingressRule := createIngressRule(name, "", "", domainSuffix, ingress, *ingress.Spec.Backend, tls)
		out = append(out, ingressRule)
	}

	for i, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			log.Warnf("invalid ingress rule for host %q, no paths defined", rule.Host)
			continue
		}
		for j, path := range rule.HTTP.Paths {
			name := EncodeIngressRuleName(ingress.Name, i+1, j+1)
			ingressRule := createIngressRule(name, rule.Host, path.Path,
				domainSuffix, ingress, path.Backend, tls)
			out = append(out, ingressRule)
		}
	}
	return out
}

func createIngressRule(name, host, path, domainSuffix string,
	ingress v1beta1.Ingress, backend v1beta1.IngressBackend, tlsSecret string) model.Config {
	rule := &routing.IngressRule{
		Destination: &routing.IstioService{
			Name: backend.ServiceName,
		},
		TlsSecret: tlsSecret,
		Match: &routing.MatchCondition{
			Request: &routing.MatchRequest{
				Headers: make(map[string]*routing.StringMatch, 2),
			},
		},
	}
	switch backend.ServicePort.Type {
	case intstr.Int:
		rule.DestinationServicePort = &routing.IngressRule_DestinationPort{
			DestinationPort: int32(backend.ServicePort.IntValue()),
		}
	case intstr.String:
		rule.DestinationServicePort = &routing.IngressRule_DestinationPortName{
			DestinationPortName: backend.ServicePort.String(),
		}
	}

	if host != "" {
		rule.Match.Request.Headers[model.HeaderAuthority] = &routing.StringMatch{
			MatchType: &routing.StringMatch_Exact{Exact: host},
		}
	}

	if path != "" {
		if strings.HasSuffix(path, ".*") {
			rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
				MatchType: &routing.StringMatch_Prefix{Prefix: strings.TrimSuffix(path, ".*")},
			}
		} else {
			rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
				MatchType: &routing.StringMatch_Exact{Exact: path},
			}
		}
	} else {
		rule.Match.Request.Headers[model.HeaderURI] = &routing.StringMatch{
			MatchType: &routing.StringMatch_Prefix{Prefix: "/"},
		}
	}

	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            model.IngressRule.Type,
			Group:           crd.ResourceGroup(&model.IngressRule),
			Version:         model.IngressRule.Version,
			Name:            name,
			Namespace:       ingress.Namespace,
			Domain:          domainSuffix,
			Labels:          ingress.Labels,
			Annotations:     ingress.Annotations,
			ResourceVersion: ingress.ResourceVersion,
		},
		Spec: rule,
	}
}

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

// ConvertIngressV1alpha3 converts from ingress spec to Istio Gateway + VirtualServices
func ConvertIngressV1alpha3(ingress v1beta1.Ingress, domainSuffix string) (model.Config, model.Config) {

	gateway := &networking.Gateway{
		Selector: model.IstioIngressWorkloadLabels,
	}

	for _, tls := range ingress.Spec.TLS {
		gateway.Servers = append(gateway.Servers, &networking.Server{
			Port: &networking.Port{
				Number:   443,
				Protocol: string(model.ProtocolHTTPS),
				Name:     "https-ingress-443",
			},
			Hosts: tls.Hosts,
			// While we accept multiple certs, we expect them to be mounted in
			// /etc/istio/certs/namespace/secretname/tls.crt|tls.key
			Tls: &networking.Server_TLSOptions{
				HttpsRedirect:     false,
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: path.Join(model.IngressCertsPath, ingress.Namespace, tls.SecretName, model.IngressCertFilename),
				CaCertificates:    path.Join(model.IngressCertsPath, ingress.Namespace, tls.SecretName, model.IngressKeyFilename),
			},
		})
	}

	gateway.Servers = append(gateway.Servers, &networking.Server{
		Port: &networking.Port{
			Number:   80,
			Protocol: string(model.ProtocolHTTP),
			Name:     "http-ingress-80",
		},
	})

	virtualService := &networking.VirtualService{
		Hosts:    []string{"*"},
		Gateways: []string{model.IstioIngressGatewayName},
	}

	var httpRoutes []*networking.HTTPRoute
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			log.Infof("invalid ingress rule for host %q, no paths defined", rule.Host)
			continue
		}

		for _, path := range rule.HTTP.Paths {
			httpMatch := &networking.HTTPMatchRequest{
				Uri: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: path.Path,
					},
				},
				Authority: &networking.StringMatch{
					MatchType: &networking.StringMatch_Regex{
						Regex: rule.Host,
					},
				},
			}

			httpRoute := ingressBackendToHTTPRoute(&path.Backend)
			if httpRoute == nil {
				log.Infof("invalid ingress rule for host %q, no backend defined for path", rule.Host)
				continue
			}
			httpRoute.Match = []*networking.HTTPMatchRequest{httpMatch}
			httpRoutes = append(httpRoutes, httpRoute)
		}
	}

	if ingress.Spec.Backend != nil {
		httpRoutes = append(httpRoutes, ingressBackendToHTTPRoute(ingress.Spec.Backend))
	}

	virtualService.Http = httpRoutes

	gatewayConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.Gateway.Type,
			Group:     model.Gateway.Group,
			Version:   model.Gateway.Version,
			Name:      model.IstioIngressGatewayName,
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
			Name:      model.IstioIngressGatewayName,
			Namespace: model.IstioIngressNamespace,
			Domain:    domainSuffix,
		},
		Spec: virtualService,
	}

	return gatewayConfig, virtualServiceConfig

}

func ingressBackendToHTTPRoute(backend *v1beta1.IngressBackend) *networking.HTTPRoute {
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
		port.Port = &networking.PortSelector_Name{
			Name: backend.ServicePort.StrVal,
		}
	}

	return &networking.HTTPRoute{
		Route: []*networking.DestinationWeight{
			{
				Destination: &networking.Destination{
					Name: backend.ServiceName,
					Port: port,
				},
				Weight: 100,
			},
		},
	}
}

// shouldProcessIngress determines whether the given ingress resource should be processed
// by the controller, based on its ingress class annotation.
// See https://github.com/kubernetes/ingress/blob/master/examples/PREREQUISITES.md#ingress-class
func shouldProcessIngress(mesh *meshconfig.MeshConfig, ingress *v1beta1.Ingress) bool {
	class, exists := "", false
	if ingress.Annotations != nil {
		class, exists = ingress.Annotations[kube.IngressClassAnnotation]
	}

	switch mesh.IngressControllerMode {
	case meshconfig.MeshConfig_OFF:
		return false
	case meshconfig.MeshConfig_STRICT:
		return exists && class == mesh.IngressClass
	case meshconfig.MeshConfig_DEFAULT:
		return !exists || class == mesh.IngressClass
	default:
		log.Warnf("invalid ingress synchronization mode: %v", mesh.IngressControllerMode)
		return false
	}
}
