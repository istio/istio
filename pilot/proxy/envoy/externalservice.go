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
	"fmt"

	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	meshconfig "istio.io/api/mesh/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/model"
)

func buildExternalServicePort(port *routingv2.Port) *model.Port {
	protocol := model.ConvertCaseInsensitiveStringToProtocol(port.Protocol)
	return &model.Port{
		Name:     fmt.Sprintf("external-%v-%d", protocol, port.Number), // TODO: use external service port name in building model port name?
		Port:     int(port.Number),
		Protocol: protocol,
	}
}

func buildExternalServiceHTTPRoutes(mesh *meshconfig.MeshConfig, node model.Node,
	instances []*model.ServiceInstance, config model.IstioConfigStore,
	httpConfigs HTTPRouteConfigs) HTTPRouteConfigs {

	externalServiceConfigs := config.ExternalServices()
	for _, externalServiceConfig := range externalServiceConfigs {
		externalService := externalServiceConfig.Spec.(*routingv2.ExternalService)
		for _, port := range externalService.Ports {
			modelPort := buildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				httpConfig := httpConfigs.EnsurePort(modelPort.Port)
				for _, host := range externalService.Hosts {
					httpConfig.VirtualHosts = append(httpConfig.VirtualHosts,
						buildExternalServiceVirtualHost(externalService, port.Name, host, mesh, node, modelPort, instances, config))
				}
			default:
				// handled elsewhere
			}
		}
	}

	return httpConfigs.normalize()
}

func buildExternalServiceTCPListeners(mesh *meshconfig.MeshConfig, config model.IstioConfigStore) (Listeners, Clusters) {
	listeners := make(Listeners, 0)
	clusters := make(Clusters, 0)

	for _, externalServiceConfig := range config.ExternalServices() {
		externalService := externalServiceConfig.Spec.(*routingv2.ExternalService)
		for _, port := range externalService.Ports {
			modelPort := buildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis, model.ProtocolHTTPS:
				routes := make([]*TCPRoute, 0)

				switch externalService.Discovery {
				case routingv2.ExternalService_NONE:
					for _, host := range externalService.Hosts {
						cluster := buildExternalServiceCluster(host, port.Name, modelPort, nil, externalService.Discovery, nil)
						route := buildTCPRoute(cluster, []string{host})

						clusters = append(clusters, cluster)
						routes = append(routes, route)
					}
				case routingv2.ExternalService_STATIC:
					for _, host := range externalService.Hosts {
						cluster := buildExternalServiceCluster(host, port.Name, modelPort, nil, externalService.Discovery, externalService.Endpoints)
						route := buildTCPRoute(cluster, []string{host})

						clusters = append(clusters, cluster)
						routes = append(routes, route)
					}
				case routingv2.ExternalService_DNS:
					// TODO
				}

				// TODO: handle port conflicts
				routeConfig := &TCPRouteConfig{Routes: routes}
				listeners = append(listeners,
					buildTCPListener(routeConfig, WildcardAddress, int(port.Number), modelPort.Protocol))
			default:
				// handled elsewhere
			}
		}
	}

	return listeners, clusters
}

func buildExternalServiceCluster(address, endpointPortName string, port *model.Port, labels model.Labels,
	discovery routingv2.ExternalService_Discovery, endpoints []*routingv2.ExternalService_Endpoint) *Cluster {

	service := model.Service{Hostname: address}
	key := service.Key(port, labels)
	clusterName := OutboundClusterPrefix + truncateClusterName(key)

	// will only be populated with discovery type dns or static
	// TODO: make sure the endpoint types are validated (in validation.go)
	// TODO: port/labels
	hosts := make([]Host, 0, len(endpoints))
	for _, endpoint := range endpoints {
		var found bool
		for name, portNumber := range endpoint.Ports {
			if name == endpointPortName {
				url := fmt.Sprintf("tcp://%s:%d", endpoint.Address, int(portNumber))
				hosts = append(hosts, Host{URL: url})

				found = true
				break
			}
		}

		if !found {
			url := fmt.Sprintf("tcp://%s:%d", endpoint.Address, int(port.Port))
			hosts = append(hosts, Host{URL: url})
		}
	}

	var clusterType, lbType string
	switch discovery {
	case routingv2.ExternalService_DNS:
		clusterType = ClusterTypeStrictDNS // strict_dns
		lbType = LbTypeRoundRobin
	case routingv2.ExternalService_STATIC:
		clusterType = ClusterTypeStatic // use endpoints
		lbType = LbTypeRoundRobin
	default: // None or unsupported discovery type
		clusterType = ClusterTypeOriginalDST // original dst cluster
		lbType = LbTypeOriginalDST
	}

	var features string
	switch port.Protocol {
	case model.ProtocolHTTP2, model.ProtocolGRPC:
		features = ClusterFeatureHTTP2
	}

	return &Cluster{
		Name:             clusterName,
		ServiceName:      key,
		ConnectTimeoutMs: 0, // FIXME: use mesh config value
		Type:             clusterType,
		LbType:           lbType,
		MaxRequestsPerConnection: 0,
		Hosts:            hosts,
		SSLContext:       nil,
		Features:         features,
		CircuitBreaker:   nil,
		OutlierDetection: nil,
		outbound:         true,
		hostname:         address,
		port:             port,
		labels:           labels,
	}
}

func buildExternalServiceVirtualHost(externalService *routingv2.ExternalService, portName, destination string,
	mesh *meshconfig.MeshConfig, sidecar model.Node, port *model.Port, instances []*model.ServiceInstance,
	config model.IstioConfigStore) *VirtualHost {

	cluster := buildExternalServiceCluster(destination, portName, port, nil, externalService.Discovery, externalService.Endpoints)

	// TODO: handle conflict between discovery type none + weighted routes?
	routes := buildDestinationHTTPRoutes(sidecar, &model.Service{Hostname: destination}, port, instances, config)

	// Set the destination clusters to the cluster we computed above.
	// Discovery type none
	for _, route := range routes {
		// redirect rules must have empty Cluster name
		if !route.Redirect() {
			route.Cluster = cluster.Name
		}
		// cluster for default route must be defined
		route.clusters = []*Cluster{cluster}
	}

	virtualHostName := fmt.Sprintf("%s:%d", destination, port.Port)
	return &VirtualHost{
		Name:    virtualHostName,
		Domains: appendPortToDomains([]string{destination}, port.Port),
		Routes:  routes,
	}
}
