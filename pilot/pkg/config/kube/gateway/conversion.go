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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	k8s "sigs.k8s.io/service-apis/apis/v1alpha1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/pkg/log"
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
	GatewayClass []config.Config
	Gateway      []config.Config
	HTTPRoute    []config.Config
	TCPRoute     []config.Config
	Namespaces   map[string]*corev1.Namespace

	// Domain for the cluster. Typically cluster.local
	Domain string
}

func isRouteMatch(cfg config.Config, res resource.Schema, gatewayNamespace string,
	routes k8s.RouteBindingSelector, namespaces map[string]*corev1.Namespace) bool {
	if routes.Resource != res.Plural() {
		return false
	}
	if routes.Group != "" && routes.Group != res.Group() {
		return false
	}
	ls, err := metav1.LabelSelectorAsSelector(&routes.RouteSelector)
	if err != nil {
		log.Errorf("failed to create route selector: %v", err)
		return false
	}
	if !ls.Matches(klabels.Set(cfg.Labels)) {
		return false
	}
	ns, err := metav1.LabelSelectorAsSelector(&routes.RouteNamespaces.NamespaceSelector)
	if err != nil {
		log.Errorf("failed to create namespace selector: %v", err)
		return false
	}

	if routes.RouteNamespaces.OnlySameNamespace {
		if gatewayNamespace != cfg.Namespace {
			return false
		}
	} else if !ns.Empty() {
		namespace := namespaces[cfg.Namespace]
		if namespace == nil {
			log.Errorf("missing namespace %v for route %v, skipping", cfg.Namespace, cfg.Name)
			return false
		}
		if !ns.Matches(klabels.Set(namespace.Labels)) {
			return false
		}
	}
	return true
}

func (r *KubernetesResources) fetchHTTPRoutes(gatewayNamespace string, routes k8s.RouteBindingSelector) []config.Config {
	result := []config.Config{}
	for _, http := range r.HTTPRoute {
		if isRouteMatch(http, collections.K8SServiceApisV1Alpha1Httproutes.Resource(), gatewayNamespace, routes, r.Namespaces) {
			result = append(result, http)
		}
	}
	return result
}

func (r *KubernetesResources) fetchTCPRoutes(gatewayNamespace string, routes k8s.RouteBindingSelector) []config.Config {
	result := []config.Config{}
	for _, http := range r.TCPRoute {
		if isRouteMatch(http, collections.K8SServiceApisV1Alpha1Tcproutes.Resource(), gatewayNamespace, routes, r.Namespaces) {
			result = append(result, http)
		}
	}
	return result
}

type IstioResources struct {
	Gateway        []config.Config
	VirtualService []config.Config
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

// Unique key to identify a route
type RouteKey struct {
	Gvk       config.GroupVersionKind
	Name      string
	Namespace string
}

func toRouteKey(c config.Config) RouteKey {
	return RouteKey{
		c.GroupVersionKind,
		c.Name,
		c.Namespace,
	}
}

func convertVirtualService(r *KubernetesResources, routeMap map[RouteKey][]string) []config.Config {
	result := []config.Config{}
	for _, obj := range r.TCPRoute {
		gateways, f := routeMap[toRouteKey(obj)]
		if !f {
			// There are no gateways using this route
			continue
		}

		vsConfig := buildTCPVirtualService(obj, gateways, r.Domain)
		result = append(result, vsConfig)
	}

	for _, obj := range r.HTTPRoute {
		gateways, f := routeMap[toRouteKey(obj)]
		if !f {
			// There are no gateways using this route
			continue
		}

		result = append(result, buildHTTPVirtualServices(obj, gateways, r.Domain)...)
	}
	return result
}

func buildHTTPVirtualServices(obj config.Config, gateways []string, domain string) []config.Config {
	result := []config.Config{}

	route := obj.Spec.(*k8s.HTTPRouteSpec)
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
				vs.Route = buildHTTPDestination(r.Action, obj.Namespace)
			}
			httproutes = append(httproutes, vs)
		}
		vsConfig := config.Config{
			Meta: config.Meta{
				GroupVersionKind: istioVsResource.GroupVersionKind(),
				Name:             name,
				Namespace:        obj.Namespace,
				Domain:           domain,
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

func buildTCPVirtualService(obj config.Config, gateways []string, domain string) config.Config {
	route := obj.Spec.(*k8s.TCPRouteSpec)
	routes := []*istio.TCPRoute{}
	for _, r := range route.Rules {
		ir := &istio.TCPRoute{
			Match: buildTCPMatch(r.Match),
			Route: buildTCPDestination(r.Action, obj.Namespace),
		}
		routes = append(routes, ir)
	}

	vsConfig := config.Config{
		Meta: config.Meta{
			GroupVersionKind: istioVsResource.GroupVersionKind(),
			Name:             fmt.Sprintf("%s-tcp-%s", obj.Name, constants.KubernetesGatewayName),
			Namespace:        obj.Namespace,
			Domain:           domain,
		},
		Spec: &istio.VirtualService{
			// TODO investigate if we should/muust constrain this to avoid conflicts
			Hosts:    []string{"*"},
			Gateways: gateways,
			Tcp:      routes,
		},
	}
	return vsConfig
}

func buildTCPDestination(action *k8s.TCPRouteAction, ns string) []*istio.RouteDestination {
	if action == nil || action.ForwardTo == nil {
		return nil
	}

	// TODO TCP currently has weight associated but only a single action. Update this one its changed to
	// either be a list or has no weight
	return []*istio.RouteDestination{{
		Destination: buildDestination(*action.ForwardTo, ns),
	}}
}

func buildTCPMatch(*k8s.TCPRouteMatch) []*istio.L4MatchAttributes {
	// Currently the spec only supports extensions, which are not currently implemented by Istio.
	return nil
}

func intSum(n []int) int {
	r := 0
	for _, i := range n {
		r += i
	}
	return r
}

func buildHTTPDestination(action *k8s.HTTPRouteAction, ns string) []*istio.HTTPRouteDestination {
	if action == nil || len(action.ForwardTo) == 0 {
		return nil
	}

	if len(action.ForwardTo) == 1 {
		return []*istio.HTTPRouteDestination{{
			Destination: buildDestination(action.ForwardTo[0], ns),
		}}
	}

	weights := []int{}
	for _, w := range action.ForwardTo {
		weights = append(weights, int(w.Weight))
	}
	weights = standardizeWeights(weights)
	res := []*istio.HTTPRouteDestination{}
	for i, fwd := range action.ForwardTo {
		dst := buildDestination(fwd, ns)
		res = append(res, &istio.HTTPRouteDestination{
			Destination: dst,
			Weight:      int32(weights[i]),
		})
	}
	return res
}

func buildDestination(to k8s.ForwardToTarget, ns string) *istio.Destination {
	res := &istio.Destination{}
	if to.TargetPort != nil {
		res.Port = &istio.PortSelector{Number: uint32(*to.TargetPort)}
	}
	// Referencing a Service or default
	if (to.TargetRef.Group == "core" || to.TargetRef.Group == "") &&
		(to.TargetRef.Resource == k8sServiceResource.Plural() || to.TargetRef.Resource == "") {
		res.Host = fmt.Sprintf("%s.%s.svc.%s", to.TargetRef.Name, ns, constants.DefaultKubernetesDomain)
	} else {
		log.Errorf("referencing unsupported destination %+v", to.TargetRef)
	}
	return res
}

// standardizeWeights migrates a list of weights from relative weights, to weights out of 100
// In the event we cannot cleanly move to 100 denominator, we will round up weights in order. See test for details.
// TODO in the future we should probably just make VirtualService support relative weights directly
func standardizeWeights(weights []int) []int {
	total := intSum(weights)
	results := make([]int, 0, len(weights))
	remainders := make([]float64, 0, len(weights))
	for _, w := range weights {
		perc := float64(w) / float64(total)
		rounded := int(perc * 100)
		remainders = append(remainders, (perc*100)-float64(rounded))
		results = append(results, rounded)
	}
	remaining := 100 - intSum(results)
	order := argsort(remainders)
	for _, idx := range order {
		if remaining <= 0 {
			break
		}
		remaining--
		results[idx]++
	}
	return results
}

type argSlice struct {
	sort.Interface
	idx []int
}

func (s argSlice) Swap(i, j int) {
	s.Interface.Swap(i, j)
	s.idx[i], s.idx[j] = s.idx[j], s.idx[i]
}

func argsort(n []float64) []int {
	s := &argSlice{Interface: sort.Float64Slice(n), idx: make([]int, len(n))}
	for i := range s.idx {
		s.idx[i] = i
	}
	sort.Sort(sort.Reverse(s))
	return s.idx
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
			log.Warnf("unknown type: %v is not supported HeaderMatchType", match.HeaderMatchType)
			return nil
		}
	}
	return res
}

func createURIMatch(match *k8s.HTTPRouteMatch) *istio.StringMatch {
	if match.Path == nil {
		return nil
	}
	if match.PathMatchType == "" || match.PathMatchType == k8s.PathMatchExact {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Exact{Exact: *match.Path},
		}
	} else if match.PathMatchType == k8s.PathMatchPrefix {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Prefix{Prefix: *match.Path},
		}
	} else if match.PathMatchType == k8s.PathMatchRegularExpression {
		return &istio.StringMatch{
			MatchType: &istio.StringMatch_Regex{Regex: *match.Path},
		}
	} else {
		log.Warnf("unknown type: %s is not supported PathMatchType", match.PathMatchType)
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

func convertGateway(r *KubernetesResources) ([]config.Config, map[RouteKey][]string) {
	result := []config.Config{}
	routeToGateway := map[RouteKey][]string{}
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
				k := toRouteKey(http)
				routeToGateway[k] = append(routeToGateway[k], obj.Namespace+"/"+name)
			}
			for _, tcp := range r.fetchTCPRoutes(obj.Namespace, l.Routes) {
				k := toRouteKey(tcp)
				routeToGateway[k] = append(routeToGateway[k], obj.Namespace+"/"+name)
			}
		}
		gatewayConfig := config.Config{
			Meta: config.Meta{
				GroupVersionKind: istioGwResource.GroupVersionKind(),
				Name:             name,
				Namespace:        obj.Namespace,
				Domain:           r.Domain,
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
