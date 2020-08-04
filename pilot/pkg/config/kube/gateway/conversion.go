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

package gateway

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	k8s "sigs.k8s.io/service-apis/apis/v1alpha1"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"

	istio "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
)

const (
	ControllerName = "istio.io/gateway-controller"
)

var (
	istioVsResource = collections.IstioNetworkingV1Alpha3Virtualservices.Resource()
	istioGwResource = collections.IstioNetworkingV1Alpha3Gateways.Resource()

	k8sServiceResource = collections.K8SCoreV1Services.Resource()
)

type KubernetesResources struct {
	GatewayClass []model.Config
	Gateway      []model.Config
	HTTPRoute    []model.Config
	TCPRoute     []model.Config
	TrafficSplit []model.Config
	Namespaces   map[string]*corev1.Namespace

	// Domain for the cluster. Typically cluster.local
	Domain string
}

func (r *KubernetesResources) fetchHTTPRoutes(gatewayNamespace string, routes k8s.RouteBindingSelector) []*k8s.HTTPRouteSpec {
	ls, err := metav1.LabelSelectorAsSelector(&routes.RouteSelector)
	if err != nil {
		log.Errorf("failed to create route selector: %v", err)
		return nil
	}
	ns, err := metav1.LabelSelectorAsSelector(&routes.RouteNamespaces.NamespaceSelector)
	if err != nil {
		log.Errorf("failed to create namespace selector: %v", err)
		return nil
	}
	result := []*k8s.HTTPRouteSpec{}
	for _, http := range r.HTTPRoute {
		if ls.Matches(klabels.Set(http.Labels)) {
			if routes.RouteNamespaces.OnlySameNamespace {
				if gatewayNamespace != http.Namespace {
					continue
				}
			} else if !ns.Empty() {
				namespace := r.Namespaces[http.Namespace]
				if namespace == nil {
					log.Errorf("missing namespace %v for route %v, skipping", http.Namespace, http.Name)
					continue
				}
				if !ns.Matches(klabels.Set(namespace.Labels)) {
					continue
				}
			}
			result = append(result, http.Spec.(*k8s.HTTPRouteSpec))
		}
	}

	return result
}

type IstioResources struct {
	Gateway        []model.Config
	VirtualService []model.Config
}

var _ = k8s.HTTPRoute{}

func convertResources(r *KubernetesResources) IstioResources {
	result := IstioResources{}
	gw, routeMap := convertGateway(r)
	vs := convertVirtualService(r, routeMap)
	result.Gateway = gw
	result.VirtualService = vs
	return result
}

func convertVirtualService(r *KubernetesResources, routeMap map[*k8s.HTTPRouteSpec][]string) []model.Config {
	result := []model.Config{}
	// TODO implement this once the API does. For now just iterate to make sure types work
	for _, obj := range r.TrafficSplit {
		_ = obj.Spec.(*k8s.TrafficSplitSpec)
	}
	// TODO implement this once the API does. For now just iterate to make sure types work
	for _, obj := range r.TCPRoute {
		_ = obj.Spec.(*k8s.TcpRouteSpec)
	}
	for _, obj := range r.HTTPRoute {
		route := obj.Spec.(*k8s.HTTPRouteSpec)

		gateways, f := routeMap[route]
		if !f {
			// There are no gateways using this route
			continue
		}

		// Matches * and "/". Currently not supported - would conflict
		// with any other explicit VirtualService.
		// TODO re-evaluate this decision. Since Gateway will be bound by certain hostnames this may
		// be feasible as long as our merging logic is smart enough to put this last
		if route.Default != nil {
			log.Warnf("Ignoring default route for %v/%v", obj.Name, obj.Namespace)
		}

		for i, h := range route.Hosts {
			name := fmt.Sprintf("%s-%d-%s", obj.Name, i, constants.KubernetesGatewayName)

			httproutes := []*istio.HTTPRoute{}
			hosts := h.Hostnames
			for _, r := range h.Rules {
				// TODO: implement redirect, rewrite, timeout, mirror, corspolicy, retries
				vs := &istio.HTTPRoute{}
				if r.Match != nil {
					vs.Match = []*istio.HTTPMatchRequest{{
						Uri:     createURIMatch(r.Match),
						Headers: createHeadersMatch(r.Match),
					}}
				}
				if r.Filter != nil {
					vs.Headers = createHeadersFilter(r.Filter.Headers)
				}
				// TODO this should be required in the spec. Follow up with the service-apis team
				if r.Action != nil {
					vs.Route = createRoute(r.Action, obj.Namespace)
				}
				httproutes = append(httproutes, vs)
			}
			vsConfig := model.Config{
				ConfigMeta: model.ConfigMeta{
					GroupVersionKind: istioVsResource.GroupVersionKind(),
					Name:             name,
					Namespace:        obj.Namespace,
					Domain:           r.Domain,
				},
				Spec: &istio.VirtualService{
					Hosts:    hosts,
					Gateways: gateways,
					Http:     httproutes,
				},
			}

			result = append(result, vsConfig)
		}
	}
	return result
}

func createRoute(action *k8s.HTTPRouteAction, ns string) []*istio.HTTPRouteDestination {
	if action == nil || action.ForwardTo == nil {
		return nil
	}

	return []*istio.HTTPRouteDestination{{
		Destination: buildHTTPDestination(action.ForwardTo, ns),
	}}

}

func buildHTTPDestination(to *k8s.ForwardToTarget, ns string) *istio.Destination {
	res := &istio.Destination{}
	if to.TargetPort != nil {
		res.Port = &istio.PortSelector{Number: uint32(*to.TargetPort)}
	}
	// Referencing a Service or default
	if to.TargetRef.Group == k8sServiceResource.Group() && to.TargetRef.Resource == k8sServiceResource.Plural() ||
		to.TargetRef.Group == "" && to.TargetRef.Resource == "" {
		res.Host = fmt.Sprintf("%s.%s.svc.%s", to.TargetRef.Name, ns, constants.DefaultKubernetesDomain)
	} else {
		log.Errorf("referencing unsupported destination %v", to.TargetRef)
	}
	return res
}

func createHeadersFilter(filter *k8s.HTTPHeaderFilter) *istio.Headers {
	if filter == nil {
		return nil
	}
	return &istio.Headers{
		Request: &istio.Headers_HeaderOperations{
			Add:    filter.Add,
			Remove: filter.Remove,
		},
	}
}

func createHeadersMatch(match *k8s.HTTPRouteMatch) map[string]*istio.StringMatch {
	if len(match.Headers) == 0 {
		return nil
	}
	res := map[string]*istio.StringMatch{}
	for k, v := range match.Headers {
		if match.HeaderMatchType == nil ||
			*match.HeaderMatchType == k8s.HeaderMatchTypeExact ||
			*match.HeaderMatchType == k8s.HeaderMatchTypeImplementionSpecific {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: v},
			}
		} else {
			log.Warnf("unknown type: %s is not supported PathType", match.PathType)
			return nil
		}
	}
	return res
}

func createURIMatch(match *k8s.HTTPRouteMatch) *istio.StringMatch {
	if match.Path == nil {
		return nil
	}
	if match.PathType == "" || match.PathType == k8s.PathTypeExact {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: *match.Path},
		}
	} else if match.PathType == k8s.PathTypePrefix {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Prefix{Prefix: *match.Path},
		}
	} else if match.PathType == k8s.PathTypeRegularExpression {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Regex{Regex: *match.Path},
		}
	} else {
		log.Warnf("unknown type: %s is not supported PathType", match.PathType)
		return nil
	}
}

// getGatewayClass finds all gateway class that are owned by Istio
func getGatewayClasses(r *KubernetesResources) map[string]struct{} {
	classes := map[string]struct{}{}
	for _, obj := range r.GatewayClass {
		gwc := obj.Spec.(*k8s.GatewayClassSpec)
		if gwc.Controller == ControllerName {
			// TODO we can add any settings we need here needed for the controller
			// For now, we have none, so just add a struct
			classes[obj.Name] = struct{}{}
		}
	}
	return classes
}

func convertGateway(r *KubernetesResources) ([]model.Config, map[*k8s.HTTPRouteSpec][]string) {
	result := []model.Config{}
	routeToGateway := map[*k8s.HTTPRouteSpec][]string{}
	classes := getGatewayClasses(r)
	for _, obj := range r.Gateway {
		kgw := obj.Spec.(*k8s.GatewaySpec)
		if _, f := classes[kgw.Class]; !f {
			// No gateway class found, this may be meant for another controller; should be skipped.
			continue
		}
		name := obj.Name + "-" + constants.KubernetesGatewayName
		var servers []*istio.Server
		for _, l := range kgw.Listeners {
			server := &istio.Server{
				// Allow all hosts here. Specific routing will be determined by the virtual services
				Hosts: buildHostnameMatch(l.Hostname),
				Port: &istio.Port{
					Number: uint32(l.Port),
					// TODO currently we 1:1 support protocols in the API. If this changes we may
					// need more logic here.
					Protocol: string(l.Protocol),
					Name:     fmt.Sprintf("%v-%v-gateway-%s-%s", strings.ToLower(string(l.Protocol)), l.Port, obj.Name, obj.Namespace),
				},
				Tls: buildTLS(l.TLS),
			}

			servers = append(servers, server)

			// TODO support TCP Route
			// TODO support VirtualService
			for _, http := range r.fetchHTTPRoutes(obj.Namespace, l.Routes) {
				routeToGateway[http] = append(routeToGateway[http], obj.Namespace+"/"+name)
			}
		}
		gatewayConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind: istioGwResource.GroupVersionKind(),
				Name:             name,
				Namespace:        obj.Namespace,
				Domain:           "", // TODO hardcoded
			},
			Spec: &istio.Gateway{
				Servers: servers,
				// TODO derive this from gatewayclass param ref
				Selector: labels.Instance{constants.IstioLabel: "ingressgateway"},
			},
		}
		result = append(result, gatewayConfig)
	}
	return result, routeToGateway
}

var tlsVersionConversionMap = map[string]istio.ServerTLSSettings_TLSProtocol{
	k8s.TLS1_0: istio.ServerTLSSettings_TLSV1_0,
	k8s.TLS1_1: istio.ServerTLSSettings_TLSV1_1,
	k8s.TLS1_2: istio.ServerTLSSettings_TLSV1_2,
	k8s.TLS1_3: istio.ServerTLSSettings_TLSV1_3,
}

func buildTLS(tls *k8s.TLSConfig) *istio.ServerTLSSettings {
	if tls == nil {
		return nil
	}
	// Explicitly not supported: file mounted
	// Not yet implemented: TLS mode, https redirect, max protocol version, SANs, CipherSuites, VerifyCertificate

	// TODO: "The SNI server_name must match a route host name for the Gateway to route the TLS request."
	// Do we need to do something smarter here to support ^ ?
	out := &istio.ServerTLSSettings{
		HttpsRedirect:  false,
		Mode:           istio.ServerTLSSettings_SIMPLE,
		CredentialName: buildSecretReference(tls.CertificateRefs),
	}
	if tls.MinimumVersion != nil {
		mv, f := tlsVersionConversionMap[*tls.MinimumVersion]
		if !f {
			log.Errorf("unknonwn TLS minimum version: %v", tls.MinimumVersion)
		} else {
			out.MinProtocolVersion = mv
		}
	}
	return out
}

func buildSecretReference(refs []k8s.CertificateObjectReference) string {
	// No certs provided
	if len(refs) == 0 {
		return ""
	}
	ref := refs[0]
	if len(refs) > 1 {
		// TODO not sure how this is supposed to be implemented? Somehow needs to align with routes I think
		log.Errorf("unsupported multiple certificate references")
	}
	if (ref.Group != "" && ref.Group != "v1") || (ref.Resource != "" && ref.Resource != "secrets") {
		log.Errorf("invalid certificate reference %v, only secret is allowed", ref)
	}
	return ref.Name
}

func buildHostnameMatch(hostname k8s.HostnameMatch) []string {
	switch hostname.Match {
	case k8s.HostnameMatchDomain:
		// TODO: this does not fully meet the spec. foo.bar.<hostname> should not match.
		// Currently Istio gateway does not support this
		return []string{"*." + hostname.Name}
	case k8s.HostnameMatchExact:
		return []string{hostname.Name}
	case k8s.HostnameMatchAny:
		return []string{"*"}
	default:
		log.Errorf("unknown hostname match %v", hostname.Match)
		// TODO better error handling. Probably need to reject the whole
		return []string{"*"}
	}
}
