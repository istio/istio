// Copyright 2018 Istio Authors
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

package v1alpha3

import (
	"fmt"
	"strconv"
	"strings"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	istio_route "istio.io/istio/pilot/pkg/networking/route"
	"istio.io/istio/pilot/pkg/networking/util"
)

// buildSidecarInboundHTTPRouteConfig builds the route config with a single wildcard virtual host on the inbound path
// TODO: enable websockets, trace decorators
func (configgen *ConfigGeneratorImpl) buildSidecarInboundHTTPRouteConfig(env model.Environment,
	node model.Proxy, instance *model.ServiceInstance) *xdsapi.RouteConfiguration {

	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "",
		instance.Service.Hostname, instance.Endpoint.ServicePort)
	defaultRoute := istio_route.BuildDefaultHTTPRoute(clusterName)

	inboundVHost := route.VirtualHost{
		Name:    fmt.Sprintf("%s|http|%d", model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Port),
		Domains: []string{"*"},
		Routes:  []route.Route{*defaultRoute},
	}

	r := &xdsapi.RouteConfiguration{
		Name:             clusterName,
		VirtualHosts:     []route.VirtualHost{inboundVHost},
		ValidateClusters: &types.BoolValue{Value: false},
	}

	for _, p := range configgen.Plugins {
		in := &plugin.InputParams{
			ListenerType:    plugin.ListenerTypeHTTP,
			Env:             &env,
			Node:            &node,
			ServiceInstance: instance,
			Service:         instance.Service,
		}
		p.OnInboundRouteConfiguration(in, r)
	}

	return r
}

// BuildSidecarOutboundHTTPRouteConfig builds an outbound HTTP Route for sidecar.
func (configgen *ConfigGeneratorImpl) BuildSidecarOutboundHTTPRouteConfig(env model.Environment, node model.Proxy,
	proxyInstances []*model.ServiceInstance, services []*model.Service, routeName string) *xdsapi.RouteConfiguration {

	port := 0
	if routeName != RDSHttpProxy {
		var err error
		port, err = strconv.Atoi(routeName)
		if err != nil {
			return nil
		}
	}

	nameToServiceMap := make(map[string]*model.Service)
	for _, svc := range services {
		if port == 0 {
			nameToServiceMap[svc.Hostname] = svc
		} else {
			if svcPort, exists := svc.Ports.GetByPort(port); exists {
				nameToServiceMap[svc.Hostname] = &model.Service{
					Hostname:     svc.Hostname,
					Address:      svc.Address,
					MeshExternal: svc.MeshExternal,
					Ports:        []*model.Port{svcPort},
				}
			}
		}
	}

	// Collect all proxy labels for source match
	var proxyLabels model.LabelsCollection
	for _, w := range proxyInstances {
		proxyLabels = append(proxyLabels, w.Labels)
	}

	// Get list of virtual services bound to the mesh gateway
	virtualServices := env.VirtualServices([]string{model.IstioMeshGateway})
	guardedHosts := istio_route.TranslateVirtualHosts(virtualServices, nameToServiceMap, proxyLabels, model.IstioMeshGateway)
	vHostPortMap := make(map[int][]route.VirtualHost)

	for _, guardedHost := range guardedHosts {
		// If none of the routes matched by source, skip this guarded host
		if len(guardedHost.Routes) == 0 {
			continue
		}

		virtualHosts := make([]route.VirtualHost, 0, len(guardedHost.Hosts)+len(guardedHost.Services))
		for _, host := range guardedHost.Hosts {
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", host, guardedHost.Port),
				Domains: []string{host},
				Routes:  guardedHost.Routes,
			})
		}

		for _, svc := range guardedHost.Services {
			domains := []string{svc.Hostname, fmt.Sprintf("%s:%d", svc.Hostname, guardedHost.Port)}
			if !svc.MeshExternal {
				domains = append(domains, generateAltVirtualHosts(svc.Hostname, guardedHost.Port)...)
			}
			if len(svc.Address) > 0 {
				// add a vhost match for the IP (if its non CIDR)
				cidr := util.ConvertAddressToCidr(svc.Address)
				if cidr.PrefixLen.Value == 32 {
					domains = append(domains, svc.Address)
					domains = append(domains, fmt.Sprintf("%s:%d", svc.Address, guardedHost.Port))
				}
			}
			virtualHosts = append(virtualHosts, route.VirtualHost{
				Name:    fmt.Sprintf("%s:%d", svc.Hostname, guardedHost.Port),
				Domains: domains,
				Routes:  guardedHost.Routes,
			})
		}

		vHostPortMap[guardedHost.Port] = append(vHostPortMap[guardedHost.Port], virtualHosts...)
	}

	var virtualHosts []route.VirtualHost
	if routeName == RDSHttpProxy {
		virtualHosts = mergeAllVirtualHosts(vHostPortMap)
	} else {
		virtualHosts = vHostPortMap[port]
	}

	out := &xdsapi.RouteConfiguration{
		Name:             fmt.Sprintf("%d", port),
		VirtualHosts:     virtualHosts,
		ValidateClusters: &types.BoolValue{Value: false},
	}

	// call plugins
	for _, p := range configgen.Plugins {
		in := &plugin.InputParams{
			ListenerType: plugin.ListenerTypeHTTP,
			Env:          &env,
			Node:         &node,
		}
		p.OnOutboundRouteConfiguration(in, out)
	}

	return out
}

// Given a service, and a port, this function generates all possible HTTP Host headers.
// For example, a service of the form foo.local.campus.net on port 80 could be accessed as
// http://foo:80 within the .local network, as http://foo.local:80 (by other clients in the campus.net domain),
// as http://foo.local.campus:80, etc.
func generateAltVirtualHosts(hostname string, port int) []string {
	var vhosts []string
	for i := len(hostname) - 1; i >= 0; i-- {
		if hostname[i] == '.' {
			variant := hostname[:i]
			variantWithPort := fmt.Sprintf("%s:%d", variant, port)
			vhosts = append(vhosts, variant)
			vhosts = append(vhosts, variantWithPort)
		}
	}
	return vhosts
}

// mergeAllVirtualHosts across all ports. On routes for ports other than port 80,
// virtual hosts without an explicit port suffix (IP:PORT) should be stripped
func mergeAllVirtualHosts(vHostPortMap map[int][]route.VirtualHost) []route.VirtualHost {
	var virtualHosts []route.VirtualHost
	for p, vhosts := range vHostPortMap {
		if p == 80 {
			virtualHosts = append(virtualHosts, vhosts...)
		} else {
			for _, vhost := range vhosts {
				var newDomains []string
				for _, domain := range vhost.Domains {
					if strings.Contains(domain, ":") {
						newDomains = append(newDomains, domain)
					}
				}
				if len(newDomains) > 0 {
					vhost.Domains = newDomains
					virtualHosts = append(virtualHosts, vhost)
				}
			}
		}
	}
	return virtualHosts
}
