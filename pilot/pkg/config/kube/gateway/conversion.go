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
	k8s "sigs.k8s.io/service-apis/api/v1alpha1"

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
}

func (r *KubernetesResources) fetchHTTPRoutes(routes k8s.RouteBindingSelector) []*k8s.HTTPRouteSpec {
	ls, err := metav1.LabelSelectorAsSelector(routes.RouteSelector)
	if err != nil {
		log.Errorf("failed to create route selector: %v", err)
		return nil
	}
	// TODO(https://github.com/kubernetes-sigs/service-apis/issues/197)
	// Selecting namespaces is problematic
	ns, err := metav1.LabelSelectorAsSelector(&routes.NamespaceSelector)
	if err != nil {
		log.Errorf("failed to create namespace selector: %v", err)
		return nil
	}
	result := []*k8s.HTTPRouteSpec{}
	for _, http := range r.HTTPRoute {
		if routes.RouteSelector == nil || ls.Matches(klabels.Set(http.Labels)) {
			if !ns.Empty() {
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
		name := obj.Name + "-" + constants.KubernetesGatewayName

		gateways, f := routeMap[route]
		if !f {
			continue
		}

		httproutes := []*istio.HTTPRoute{}
		hosts := []string{}
		for _, h := range route.Hosts {
			// TODO does flattening here work?
			hosts = append(hosts, h.Hostname)
			for _, r := range h.Rules {
				vs := &istio.HTTPRoute{
					Match:            nil,
					Route:            nil,
					Redirect:         nil,
					Rewrite:          nil,
					Timeout:          nil,
					Retries:          nil,
					Fault:            nil,
					Mirror:           nil,
					MirrorPercentage: nil,
					CorsPolicy:       nil,
					//Headers:               nil,
				}
				if r.Match != nil {
					vs.Match = []*istio.HTTPMatchRequest{{
						Uri:     createURIMatch(r.Match),
						Headers: createHeadersMatch(r.Match),
					}}
				}
				if r.Filter != nil {
					vs.Headers = createHeadersFilter(r.Filter.Headers)
				}
				// TODO this should be required? in the spec
				if r.Action != nil {
					vs.Route = createRoute(r.Action, obj.Namespace)
				}
				httproutes = append(httproutes, vs)
			}
		}
		vsConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      istioVsResource.Kind(),
				Group:     istioVsResource.Group(),
				Version:   istioVsResource.Version(),
				Name:      name,
				Namespace: obj.Namespace,
				Domain:    "", // TODO hardcoded
			},
			Spec: &istio.VirtualService{
				Hosts:    hosts,
				Gateways: gateways,
				Http:     httproutes,
			},
		}
		result = append(result, vsConfig)
	}
	return result
}

// TODO support traffic split
func createRoute(action *k8s.HTTPRouteAction, ns string) []*istio.HTTPRouteDestination {
	if action == nil || action.ForwardTo == nil {
		return nil
	}

	// TODO this is really bad. What types does object point to?
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
		log.Warnf("referencing unsupported destination %v", to.TargetRef)
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
	res := map[string]*istio.StringMatch{}
	for k, v := range match.Header {
		if match.HeaderType == nil || *match.HeaderType == k8s.HeaderTypeExact {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Exact{Exact: v},
			}
		} else if match.PathType == k8s.PathTypePrefix {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Prefix{Prefix: v},
			}
		} else if match.PathType == k8s.PathTypeRegularExpression {
			res[k] = &istio.StringMatch{
				MatchType: &istio.StringMatch_Regex{Regex: v},
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
			if l.Port == nil {
				// TODO this is optional in spec
				// TODO propagate errors to status
				log.Warnf("invalid listener, port is nil: %v", l)
				continue
			}
			if l.Protocol == nil {
				// TODO this is optional in spec
				// TODO propagate errors to status
				log.Warnf("invalid listener, protocol is nil: %v", l)
				continue
			}
			server := &istio.Server{
				Port: &istio.Port{
					Number:   uint32(*l.Port),
					Protocol: *l.Protocol,
					Name:     fmt.Sprintf("%v-%v-gateway-%s-%s", strings.ToLower(*l.Protocol), *l.Port, obj.Name, obj.Namespace),
				},
			}
			if l.Address == nil {
				server.Hosts = []string{"*"}
			} else {
				// TODO: support addressType
				server.Hosts = []string{l.Address.Value}
			}

			servers = append(servers, server)
		}
		// TODO support TCP Route
		// TODO support VirtualService
		for _, http := range r.fetchHTTPRoutes(kgw.Routes) {
			routeToGateway[http] = append(routeToGateway[http], name)
		}
		gatewayConfig := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      istioGwResource.Kind(),
				Group:     istioGwResource.Group(),
				Version:   istioGwResource.Version(),
				Name:      name,
				Namespace: obj.Namespace,
				Domain:    "", // TODO hardcoded
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
