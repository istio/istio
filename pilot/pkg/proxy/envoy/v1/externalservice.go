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

package v1

import (
	"fmt"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func buildExternalServicePort(port *networking.Port) *model.Port {
	protocol := model.ConvertCaseInsensitiveStringToProtocol(port.Protocol)
	return &model.Port{
		Name:     fmt.Sprintf("external-%v-%d", protocol, port.Number), // TODO: use external service port name in building model port name?
		Port:     int(port.Number),
		Protocol: protocol,
	}
}

func buildExternalServiceHTTPRoutes(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore,
	httpConfigs HTTPRouteConfigs) HTTPRouteConfigs {

	externalServiceConfigs := config.ExternalServices()
	for _, externalServiceConfig := range externalServiceConfigs {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)
		meshName := externalServiceConfig.Name + "." + externalServiceConfig.Namespace +
			"." + externalServiceConfig.Domain
		for _, port := range externalService.Ports {
			modelPort := buildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				httpConfig := httpConfigs.EnsurePort(modelPort.Port)
				for _, host := range externalService.Hosts {
					httpConfig.VirtualHosts = append(httpConfig.VirtualHosts,
						buildExternalServiceVirtualHost(meshName, externalService, port.Name, host, mesh,
							node, modelPort, proxyInstances, config))
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
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)
		for _, port := range externalService.Ports {
			modelPort := buildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis, model.ProtocolHTTPS:
				routes := make([]*TCPRoute, 0)

				for _, host := range externalService.Hosts {
					cluster := buildExternalServiceCluster(mesh, host, port.Name, modelPort, nil,
						externalService.Discovery, externalService.Endpoints)
					route := buildTCPRoute(cluster, []string{host})

					clusters = append(clusters, cluster)
					routes = append(routes, route)
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

func buildExternalServiceCluster(mesh *meshconfig.MeshConfig,
	address, endpointPortName string, port *model.Port, labels model.Labels,
	discovery networking.ExternalService_Discovery, endpoints []*networking.ExternalService_Endpoint) *Cluster {

	service := model.Service{Hostname: address}
	key := service.Key(port, labels)
	clusterName := TruncateClusterName(OutboundClusterPrefix + key)

	// will only be populated with discovery type dns or static
	hosts := make([]Host, 0)
	for _, endpoint := range endpoints {
		if !labels.SubsetOf(model.Labels(endpoint.Labels)) {
			continue
		}

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
			// default to the external service port
			url := fmt.Sprintf("tcp://%s:%d", endpoint.Address, port.Port)
			hosts = append(hosts, Host{URL: url})
		}
	}

	// Use host address if discovery type DNS and no endpoints are provided
	if discovery == networking.ExternalService_DNS && len(endpoints) == 0 {
		url := fmt.Sprintf("tcp://%s:%d", address, port.Port)
		hosts = append(hosts, Host{URL: url})
	}

	var clusterType, lbType string
	switch discovery {
	case networking.ExternalService_NONE:
		clusterType = ClusterTypeOriginalDST
		lbType = LbTypeOriginalDST
	case networking.ExternalService_DNS:
		clusterType = ClusterTypeStrictDNS
		lbType = LbTypeRoundRobin
	case networking.ExternalService_STATIC:
		clusterType = ClusterTypeStatic
		lbType = LbTypeRoundRobin
	}

	var sslContext interface{}
	if port.Protocol == model.ProtocolHTTPS {
		sslContext = &SSLContextExternal{}
	}

	var features string
	switch port.Protocol {
	case model.ProtocolHTTP2, model.ProtocolGRPC:
		features = ClusterFeatureHTTP2
	}

	return &Cluster{
		Name:             clusterName,
		ServiceName:      key,
		ConnectTimeoutMs: protoDurationToMS(mesh.ConnectTimeout),
		Type:             clusterType,
		LbType:           lbType,
		Hosts:            hosts,
		SSLContext:       sslContext,
		Features:         features,
		outbound:         true,
		hostname:         address,
		port:             port,
		labels:           labels,
	}
}

// buildExternalServiceVirtualHost from the perspective of the 'sidecar' node.
func buildExternalServiceVirtualHost(serviceName string, externalService *networking.ExternalService, portName, destination string,
	mesh *meshconfig.MeshConfig, node model.Proxy, port *model.Port, proxyInstances []*model.ServiceInstance,
	config model.IstioConfigStore) *VirtualHost {

	service := &model.Service{Hostname: destination}
	buildClusterFunc := func(hostname string, port *model.Port, labels model.Labels, isExternal bool) *Cluster {
		return buildExternalServiceCluster(mesh, hostname, portName, port, labels,
			externalService.Discovery, externalService.Endpoints)
	}

	// FIXME: clusters generated if the routing rule routes traffic to other services will be constructed incorrectly
	// FIXME: similarly, routing rules for other services that route to this external service will be constructed incorrectly
	routes := buildDestinationHTTPRoutes(node, service, port, proxyInstances, config, buildClusterFunc)

	// inject Mixer calls per route.
	// every route here belongs to the same destination.service, ie serviceName
	// And source is the sidecar All attributes are directly sent to Mixer so none are forwarded.
	if mesh.MixerCheckServer != "" || mesh.MixerReportServer != "" {
		oc := buildMixerConfig(node, serviceName, service, proxyInstances, config, mesh.DisablePolicyChecks, false)
		for _, route := range routes {
			route.OpaqueConfig = oc
		}
	}

	virtualHostName := fmt.Sprintf("%s:%d", destination, port.Port)
	return &VirtualHost{
		Name:    virtualHostName,
		Domains: appendPortToDomains([]string{destination}, port.Port),
		Routes:  routes,
	}
}
