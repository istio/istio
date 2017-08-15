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
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

func buildIngressListeners(mesh *proxyconfig.ProxyMeshConfig,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore,
	ingress proxy.Node) Listeners {
	listeners := Listeners{
		buildHTTPListener(mesh, ingress, nil, WildcardAddress, 80, true, true),
	}

	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := buildIngressRoutes(mesh, discovery, config)
	if secret != "" {
		listener := buildHTTPListener(mesh, ingress, nil, WildcardAddress, 443, true, true)
		listener.SSLContext = &SSLContext{
			CertChainFile:  path.Join(proxy.IngressCertsPath, "tls.crt"),
			PrivateKeyFile: path.Join(proxy.IngressCertsPath, "tls.key"),
		}
		listeners = append(listeners, listener)
	}

	return listeners
}

func buildIngressRoutes(mesh *proxyconfig.ProxyMeshConfig,
	discovery model.ServiceDiscovery,
	config model.IstioConfigStore) (HTTPRouteConfigs, string) {
	ingressRules := config.IngressRules()

	// build vhosts
	vhosts := make(map[string][]*HTTPRoute)
	vhostsTLS := make(map[string][]*HTTPRoute)
	tlsAll := ""

	// skip over source-matched route rules
	rules := config.RouteRulesBySource(nil)

	for _, rule := range ingressRules {
		routes, tls, err := buildIngressRoute(mesh, rule, discovery, rules)
		if err != nil {
			glog.Warningf("Error constructing Envoy route from ingress rule: %v", err)
			continue
		}

		host := "*"
		if rule.Match != nil {
			if authority, ok := rule.Match.HttpHeaders[model.HeaderAuthority]; ok {
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
			Domains: []string{host},
			Routes:  routes,
		})
	}

	rcTLS := &HTTPRouteConfig{VirtualHosts: make([]*VirtualHost, 0)}
	for host, routes := range vhostsTLS {
		sort.Sort(RoutesByPath(routes))
		rcTLS.VirtualHosts = append(rcTLS.VirtualHosts, &VirtualHost{
			Name:    host,
			Domains: []string{host},
			Routes:  routes,
		})
	}

	configs := HTTPRouteConfigs{80: rc, 443: rcTLS}
	configs.normalize()
	return configs, tlsAll
}

// buildIngressRoute translates an ingress rule to an Envoy route
func buildIngressRoute(mesh *proxyconfig.ProxyMeshConfig, ingress *proxyconfig.IngressRule,
	discovery model.ServiceDiscovery, rules []*proxyconfig.RouteRule) ([]*HTTPRoute, string, error) {
	service, exists := discovery.GetService(ingress.Destination)
	if !exists {
		return nil, "", fmt.Errorf("cannot find service %q", ingress.Destination)
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
	routes := buildDestinationHTTPRoutes(service, servicePort, rules)

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
			route.OpaqueConfig = buildMixerOpaqueConfig(true, true)
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
