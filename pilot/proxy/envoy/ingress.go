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

package envoy

import (
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
)

func buildIngressListeners(mesh *proxyconfig.MeshConfig,
	instances []*model.ServiceInstance,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore,
	ingress proxy.Node) Listeners {
	listeners := Listeners{
		buildHTTPListener(mesh, ingress, instances, nil, WildcardAddress, 80, "80", true, EgressTraceOperation),
	}

	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := buildIngressRoutes(mesh, instances, discovery, config)
	if secret != "" {
		listener := buildHTTPListener(mesh, ingress, instances, nil, WildcardAddress, 443, "443", true, EgressTraceOperation)
		listener.SSLContext = &SSLContext{
			CertChainFile:  path.Join(proxy.IngressCertsPath, proxy.IngressCertFilename),
			PrivateKeyFile: path.Join(proxy.IngressCertsPath, proxy.IngressKeyFilename),
		}
		listeners = append(listeners, listener)
	}

	return listeners
}

func buildIngressRoutes(mesh *proxyconfig.MeshConfig,
	instances []*model.ServiceInstance,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore) (HTTPRouteConfigs, string) {
	// build vhosts
	vhosts := make(map[string][]*HTTPRoute)
	vhostsTLS := make(map[string][]*HTTPRoute)
	tlsAll := ""

	rules, _ := config.List(model.IngressRule.Type, model.NamespaceAll)
	for _, rule := range rules {
		routes, tls, err := buildIngressRoute(mesh, instances, rule, discovery, config)
		if err != nil {
			glog.Warningf("Error constructing Envoy route from ingress rule: %v", err)
			continue
		}

		host := "*"
		ingress := rule.Spec.(*proxyconfig.IngressRule)
		if ingress.Match != nil && ingress.Match.Request != nil {
			if authority, ok := ingress.Match.Request.Headers[model.HeaderAuthority]; ok {
				switch match := authority.GetMatchType().(type) {
				case *proxyconfig.StringMatch_Exact:
					host = match.Exact
				default:
					glog.Warningf("Unsupported match type for authority condition %T, falling back to %q", match, host)
					continue
				}
			}
		}
		if tls != "" {
			vhostsTLS[host] = append(vhostsTLS[host], routes...)
			if tlsAll == "" {
				tlsAll = tls
			} else if tlsAll != tls {
				glog.Warningf("Multiple secrets detected %s and %s", tls, tlsAll)
				if tls < tlsAll {
					tlsAll = tls
				}
			}
		} else {
			vhosts[host] = append(vhosts[host], routes...)
		}
	}

	// normalize config
	rc := &HTTPRouteConfig{VirtualHosts: make([]*VirtualHost, 0)}
	for host, routes := range vhosts {
		sort.Sort(RoutesByPath(routes))
		rc.VirtualHosts = append(rc.VirtualHosts, &VirtualHost{
			Name:    host,
			Domains: buildIngressVhostDomains(host, 80),
			Routes:  routes,
		})
	}

	rcTLS := &HTTPRouteConfig{VirtualHosts: make([]*VirtualHost, 0)}
	for host, routes := range vhostsTLS {
		sort.Sort(RoutesByPath(routes))
		rcTLS.VirtualHosts = append(rcTLS.VirtualHosts, &VirtualHost{
			Name:    host,
			Domains: buildIngressVhostDomains(host, 443),
			Routes:  routes,
		})
	}

	configs := HTTPRouteConfigs{80: rc, 443: rcTLS}
	return configs.normalize(), tlsAll
}

// buildIngressVhostDomains returns an array of domain strings with the port attached
func buildIngressVhostDomains(vhost string, port int) []string {
	domains := make([]string, 0)
	domains = append(domains, vhost)

	if vhost != "*" {
		domains = append(domains, fmt.Sprintf("%s:%d", vhost, port))
	}

	return domains
}

// buildIngressRoute translates an ingress rule to an Envoy route
func buildIngressRoute(mesh *proxyconfig.MeshConfig,
	instances []*model.ServiceInstance, rule model.Config,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore) ([]*HTTPRoute, string, error) {
	ingress := rule.Spec.(*proxyconfig.IngressRule)
	destination := model.ResolveHostname(rule.ConfigMeta, ingress.Destination)
	service, err := discovery.GetService(destination)
	if err != nil {
		return nil, "", err
	}
	if service == nil {
		return nil, "", fmt.Errorf("cannot find service %q", destination)
	}
	tls := ingress.TlsSecret
	servicePort, err := extractPort(service, ingress)
	if err != nil {
		return nil, "", err
	}
	if !servicePort.Protocol.IsHTTP() {
		return nil, "", fmt.Errorf("unsupported protocol %q for %q", servicePort.Protocol, service.Hostname)
	}

	// unfold the rules for the destination port
	routes := buildDestinationHTTPRoutes(service, servicePort, instances, config)

	// filter by path, prefix from the ingress
	ingressRoute := buildHTTPRouteMatch(ingress.Match)

	// TODO: not handling header match in ingress apart from uri and authority (uri must not be regex)
	if len(ingressRoute.Headers) > 0 {
		if len(ingressRoute.Headers) > 1 || ingressRoute.Headers[0].Name != model.HeaderAuthority {
			return nil, "", errors.New("header matches in ingress rule not supported")
		}
	}

	out := make([]*HTTPRoute, 0)
	for _, route := range routes {
		// enable mixer check on the route
		if mesh.MixerAddress != "" {
			route.OpaqueConfig = buildMixerOpaqueConfig(!mesh.DisablePolicyChecks, true)
		}

		if applied := route.CombinePathPrefix(ingressRoute.Path, ingressRoute.Prefix); applied != nil {
			out = append(out, applied)
		}
	}

	return out, tls, nil
}

// extractPort extracts the destination service port from the given destination,
func extractPort(svc *model.Service, ingress *proxyconfig.IngressRule) (*model.Port, error) {
	switch p := ingress.GetDestinationServicePort().(type) {
	case *proxyconfig.IngressRule_DestinationPort:
		num := p.DestinationPort
		port, exists := svc.Ports.GetByPort(int(num))
		if !exists {
			return nil, fmt.Errorf("cannot find port %d in %q", num, svc.Hostname)
		}
		return port, nil
	case *proxyconfig.IngressRule_DestinationPortName:
		name := p.DestinationPortName
		port, exists := svc.Ports.Get(name)
		if !exists {
			return nil, fmt.Errorf("cannot find port %q in %q", name, svc.Hostname)
		}
		return port, nil
	}
	return nil, errors.New("unrecognized destination port")
}
