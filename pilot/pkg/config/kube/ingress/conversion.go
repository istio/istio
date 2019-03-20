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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
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

// ConvertIngressV1alpha3 converts from ingress spec to Istio Gateway
func ConvertIngressV1alpha3(ingress v1beta1.Ingress, domainSuffix string) model.Config {
	gateway := &networking.Gateway{
		Selector: model.IstioIngressWorkloadLabels,
	}

	// FIXME this is a temporary hack until all test templates are updated
	//for _, tls := range ingress.Spec.TLS {

	// TODO: add secretName (converted to sdsName)
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
			// /etc/istio/ingress-certs/tls.crt|tls.key|root-cert.pem
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

	gatewayConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.Gateway.Type,
			Group:     model.Gateway.Group,
			Version:   model.Gateway.Version,
			Name:      ingress.Name + "-" + model.IstioIngressGatewayName,
			Namespace: ingressNamespace,
			Domain:    domainSuffix,
		},
		Spec: gateway,
	}

	return gatewayConfig
}

// ConvertIngressVirtualService converts from ingress spec to Istio VirtualServices
func ConvertIngressVirtualService(ingress v1beta1.Ingress, domainSuffix string, ingressByHost map[string]*model.Config) {
	// Ingress allows a single host - if missing '*' is assumed
	// We need to merge all rules with a particular host across
	// all ingresses, and return a separate VirtualService for each
	// host.
	if ingressNamespace == "" {
		ingressNamespace = model.IstioIngressNamespace
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
			Hosts: []string{},
			// Note the name of the gateway is fixed - this is the Gateway that needs to be created by user (via helm
			// or manually) with TLS secrets and explicit namespace (for security).
			Gateways: []string{ingressNamespace + "/" + model.IstioIngressGatewayName},
		}

		virtualService.Hosts = []string{host}

		httpRoutes := []*networking.HTTPRoute{}
		for _, path := range rule.HTTP.Paths {
			httpMatch := &networking.HTTPMatchRequest{
				Uri: createStringMatch(path.Path),
			}

			httpRoute := ingressBackendToHTTPRoute(&path.Backend, ingress.Namespace, domainSuffix)
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
				Type:      model.VirtualService.Type,
				Group:     model.VirtualService.Group,
				Version:   model.VirtualService.Version,
				Name:      namePrefix + "-" + ingress.Name + "-" + model.IstioIngressGatewayName,
				Namespace: ingress.Namespace,
				Domain:    domainSuffix,
			},
			Spec: virtualService,
		}

		old, f := ingressByHost[host]
		if f {
			vs := old.Spec.(*networking.VirtualService)
			vs.Http = append(vs.Http, httpRoutes...)
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
