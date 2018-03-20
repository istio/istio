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
package deprecated

import (
	"fmt"
	"sort"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	_ "github.com/golang/glog" // nolint
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	alpha3 "istio.io/api/networking/v1alpha3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/log"
)

const (

	// OutboundClusterPrefix is the prefix for service clusters external to the proxy instance
	OutboundClusterPrefix = "out."

	// ClusterTypeStatic name for clusters of type 'static'
	ClusterTypeStatic = "static"

	// DefaultLbType defines the default load balancer policy
	DefaultLbType = LbTypeRoundRobin

	// LbTypeRoundRobin is the name for round-robin LB
	LbTypeRoundRobin = "round_robin"

	// ClusterFeatureHTTP2 is the feature to use HTTP/2 for a cluster
	ClusterFeatureHTTP2 = "http2"

	// ClusterTypeStrictDNS name for clusters of type 'strict_dns'
	ClusterTypeStrictDNS = "strict_dns"

	// ClusterTypeOriginalDST name for clusters of type 'original_dst'
	ClusterTypeOriginalDST = "original_dst"

	// ClusterTypeSDS name for clusters of type 'sds'
	ClusterTypeSDS = "sds"

	// LbTypeOriginalDST is the name for LB of original_dst
	LbTypeOriginalDST = "original_dst_lb"
)

// BuildClusters2 returns the list of clusters for a proxy
func BuildClusters2(env model.Environment, node model.Proxy) ([]*xdsapi.Cluster, error) {
	clusters := []*xdsapi.Cluster{}
	var proxyInstances []*model.ServiceInstance
	var err error
	switch node.Type {
	case model.Sidecar, model.Router:
		proxyInstances, err = env.GetProxyServiceInstances(node)
		if err != nil {
			return clusters, err
		}
		var services []*model.Service
		services, err = env.Services()
		if err != nil {
			return clusters, err
		}
		clusters = buildSidecarClusters2(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
	case model.Ingress:
		// TODO(v2): in progress, needs translation to v2 !!!
		//httpRouteConfigs, _ := v1.BuildIngressRoutes(env.Mesh, node, nil, env.ServiceDiscovery, env.IstioConfigStore)
		//clusters = httpRouteConfigs.Clusters().Normalize()
	}

	if err != nil {
		return clusters, err
	}

	// TODO(v2): in progress, needs translation to v2 !!!
	// apply custom policies for outbound clusters
	//for _, cluster := range clusters {
	//	v1.ApplyClusterPolicy(cluster, proxyInstances, env.IstioConfigStore, env.Mesh, env.ServiceAccounts, node.Domain)
	//}

	//// append Mixer service definition if necessary
	//if env.Mesh.MixerCheckServer != "" || env.Mesh.MixerReportServer != "" {
	//	clusters = append(clusters, v1.BuildMixerClusters(env.Mesh, node, env.MixerSAN)...)
	//	clusters = append(clusters, v1.BuildMixerAuthFilterClusters(env.IstioConfigStore, env.Mesh, proxyInstances)...)
	//}

	return clusters, nil
}

// buildSidecarListenersClusters produces a list of listeners and referenced clusters for sidecar proxies
// TODO: this implementation is inefficient as it is recomputing all the routes for all proxies
// There is a lot of potential to cache and reuse cluster definitions across proxies and also
// skip computing the actual HTTP routes
func buildSidecarClusters2(
	mesh *meshconfig.MeshConfig,
	proxyInstances []*model.ServiceInstance,
	services []*model.Service,
	managementPorts model.PortList,
	node model.Proxy,
	config model.IstioConfigStore) []*xdsapi.Cluster {

	// ensure services are ordered to simplify generation logic
	sort.Slice(services, func(i, j int) bool { return services[i].Hostname < services[j].Hostname })

	clusters := []*xdsapi.Cluster{}

	if node.Type == model.Router {
		// TODO(v2): implement (is it only for gateway ? Ingress ?)
		//outbound, outClusters := buildOutboundListeners(mesh, node, proxyInstances, services, config)
		//clusters = append(clusters, outClusters...)
	} else if mesh.ProxyListenPort > 0 {
		inClusters := buildInboundClusters2(mesh, node, proxyInstances, config)
		outClusters := buildOutboundHttpClusters2(mesh, node, proxyInstances, services, config)
		//mgmtListeners, mgmtClusters := buildMgmtPortListeners(mesh, managementPorts, node.IPAddress)

		clusters = append(clusters, inClusters...)
		clusters = append(clusters, outClusters...)

		// If management listener port and service port are same, bad things happen
		// when running in kubernetes, as the probes stop responding. So, append
		// non overlapping listeners only.
		// TODO(v2): convert to cluster only method
		//for i := range mgmtListeners {
		//	c := mgmtClusters[i]
		//	clusters = append(clusters, c)
		//}
	}

	// TODO(v2): http proxy moved to bootstrap config template

	return clusters
}

// buildOutboundListeners combines HTTP routes and TCP listeners
func buildOutboundHttpClusters2(mesh *meshconfig.MeshConfig, node model.Proxy, proxyInstances []*model.ServiceInstance,
	services []*model.Service, config model.IstioConfigStore) []*xdsapi.Cluster {

	clusters := buildOutboundTCPClusters2(mesh, node, services)

	// note that outbound HTTP routes are supplied through RDS
	//httpOutbound := buildOutboundHTTPRoutes(mesh, node, proxyInstances, services, config)
	clusters = buildExternalServiceClusters(mesh, node, proxyInstances, config, clusters)

	// TODO(v2): convert route-derived clusters
	//for _, routeConfig := range httpOutbound {
	//	clusters = append(clusters, routeConfig.Clusters()...)
	//}

	return clusters
}

// BuildExternalServiceHTTPRoutes builds clusters for external services
func buildExternalServiceClusters(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore, clusters []*xdsapi.Cluster) []*xdsapi.Cluster {

	externalServiceConfigs := config.ExternalServices()
	for _, externalServiceConfig := range externalServiceConfigs {
		externalService := externalServiceConfig.Spec.(*alpha3.ExternalService)
		//meshName := externalServiceConfig.Name + "." + externalServiceConfig.Namespace +
		//		"." + externalServiceConfig.Domain

		for _, port := range externalService.Ports {
			modelPort := v1.BuildExternalServicePort(port)
			proto := model.ConvertCaseInsensitiveStringToProtocol(port.Protocol)
			switch proto {
			case model.ProtocolHTTP, model.ProtocolHTTP2, model.ProtocolGRPC:
				for _, host := range externalService.Hosts {
					cluster := buildExternalServiceCluster(mesh, host, port.Name, modelPort, nil, externalService.Discovery, externalService.Endpoints)
					clusters = append(clusters, cluster)
				}
			case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis, model.ProtocolHTTPS:
				for _, host := range externalService.Hosts {
					cluster := buildExternalServiceCluster(mesh, host, port.Name, modelPort, nil,
						externalService.Discovery, externalService.Endpoints)

					clusters = append(clusters, cluster)
				}
			default:
				// handled elsewhere
			}
		}
	}

	return clusters
}

// BuildExternalServiceCluster builds an external service cluster.
func buildExternalServiceCluster(mesh *meshconfig.MeshConfig,
	address, endpointPortName string, port *model.Port, labels model.Labels,
	discovery alpha3.ExternalService_Discovery, endpoints []*alpha3.ExternalService_Endpoint) *xdsapi.Cluster {

	//service := model.Service{Hostname: address}
	//key := service.Key(port, labels)
	//clusterName := v1.TruncateClusterName(OutboundClusterPrefix + key)
	//
	//// will only be populated with discovery type dns or static
	//hosts := make([]Host, 0)
	//for _, endpoint := range endpoints {
	//	if !labels.SubsetOf(model.Labels(endpoint.Labels)) {
	//		continue
	//	}
	//
	//	var found bool
	//	for name, portNumber := range endpoint.Ports {
	//		if name == endpointPortName {
	//			url := fmt.Sprintf("tcp://%s:%d", endpoint.Address, int(portNumber))
	//			hosts = append(hosts, Host{URL: url})
	//
	//			found = true
	//			break
	//		}
	//	}
	//
	//	if !found {
	//		// default to the external service port
	//		url := fmt.Sprintf("tcp://%s:%d", endpoint.Address, port.Port)
	//		hosts = append(hosts, Host{URL: url})
	//	}
	//}
	//
	//// Use host address if discovery type DNS and no endpoints are provided
	//if discovery == alpha3.ExternalService_DNS && len(endpoints) == 0 {
	//	url := fmt.Sprintf("tcp://%s:%d", address, port.Port)
	//	hosts = append(hosts, Host{URL: url})
	//}
	//
	//var clusterType, lbType string
	//switch discovery {
	//case networking.ExternalService_NONE:
	//	clusterType = ClusterTypeOriginalDST
	//	lbType = LbTypeOriginalDST
	//case networking.ExternalService_DNS:
	//	clusterType = ClusterTypeStrictDNS
	//	lbType = LbTypeRoundRobin
	//case networking.ExternalService_STATIC:
	//	clusterType = ClusterTypeStatic
	//	lbType = LbTypeRoundRobin
	//}
	//
	//var sslContext interface{}
	//if port.Protocol == model.ProtocolHTTPS {
	//	sslContext = &SSLContextExternal{}
	//}
	//
	//var features string
	//switch port.Protocol {
	//case model.ProtocolHTTP2, model.ProtocolGRPC:
	//	features = ClusterFeatureHTTP2
	//}

	//return &xdsapi.Cluster{
	//	Name:             clusterName,
	//	ServiceName:      key,
	//	ConnectTimeoutMs: protoDurationToMS(mesh.ConnectTimeout),
	//	Type:             clusterType,
	//	LbType:           lbType,
	//	Hosts:            hosts,
	//	SSLContext:       sslContext,
	//	Features:         features,
	//	outbound:         true,
	//	Hostname:         address,
	//	Port:             port,
	//	labels:           labels,
	//}
	return &xdsapi.Cluster{}
}

func buildOutboundTCPClusters2(mesh *meshconfig.MeshConfig, node model.Proxy,
	services []*model.Service) []*xdsapi.Cluster {
	tcpClusters := []*xdsapi.Cluster{}

	var originalDstCluster *xdsapi.Cluster
	wildcardListenerPorts := make(map[int]bool)

	for _, service := range services {
		if service.External() {
			continue // TODO TCP external services not currently supported
		}

		for _, servicePort := range service.Ports {
			switch servicePort.Protocol {

			case model.ProtocolTCP, model.ProtocolHTTPS, model.ProtocolMongo, model.ProtocolRedis:
				if service.LoadBalancingDisabled || service.Address == "" ||
					node.Type == model.Router {
					// ensure only one wildcard listener is created per port if its headless service
					// or if its for a Router (where there is one wildcard TCP listener per port)
					// or if this is in environment where services don't get a dummy load balancer IP.
					if wildcardListenerPorts[servicePort.Port] {
						log.Debugf("Multiple definitions for port %d", servicePort.Port)
						continue
					}
					wildcardListenerPorts[servicePort.Port] = true

					var cluster *xdsapi.Cluster
					// Router mode cannot handle headless services
					if service.LoadBalancingDisabled && node.Type != model.Router {
						if originalDstCluster == nil {
							originalDstCluster = buildOriginalDSTCluster2(
								"orig-dst-cluster-tcp", mesh.ConnectTimeout)
							tcpClusters = append(tcpClusters, originalDstCluster)
						}
						cluster = originalDstCluster
					} else {
						cluster = buildOutboundCluster2(service.Hostname, servicePort, nil,
							service.External())
						tcpClusters = append(tcpClusters, cluster)
					}
				} else {
					cluster := buildOutboundCluster2(service.Hostname, servicePort, nil, service.External())
					tcpClusters = append(tcpClusters, cluster)
				}
			}
		}
	}

	return tcpClusters
}

func buildInboundClusters2(mesh *meshconfig.MeshConfig, node model.Proxy,
	proxyInstances []*model.ServiceInstance, config model.IstioConfigStore) []*xdsapi.Cluster {

	clusters := []*xdsapi.Cluster{}

	// inbound connections/requests are redirected to the endpoint address but appear to be sent
	// to the service address
	// assumes that endpoint addresses/ports are unique in the instance set
	// TODO: validate that duplicated endpoints for services can be handled (e.g. above assumption)
	for _, instance := range proxyInstances {
		endpoint := instance.Endpoint
		servicePort := endpoint.ServicePort
		protocol := servicePort.Protocol
		cluster := buildInboundCluster2(endpoint.Port, protocol, mesh.ConnectTimeout)
		clusters = append(clusters, cluster)
	}

	return clusters
}

// BuildOutboundCluster builds an outbound cluster.
func buildOutboundCluster2(hostname string, port *model.Port, labels model.Labels, isExternal bool) *xdsapi.Cluster {
	svc := model.Service{Hostname: hostname}
	key := svc.Key(port, labels)
	name := v1.TruncateClusterName(OutboundClusterPrefix + key)

	cluster := &xdsapi.Cluster{
		Name:     name,
		LbPolicy: xdsapi.Cluster_ROUND_ROBIN,
	}

	if isExternal {
		cluster.Type = xdsapi.Cluster_STRICT_DNS
		a := core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address:       hostname,
				PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(port.Port)},
			},
		}
		cluster.Hosts = []*core.Address{
			{
				Address: &a,
			},
		}
	} else {
		cluster.Type = xdsapi.Cluster_EDS

		apiSource := &core.ConfigSource_ApiConfigSource{
			ApiConfigSource: &core.ApiConfigSource{
				ApiType:      core.ApiConfigSource_GRPC,
				ClusterNames: []string{"xds-grpc"}, // used to be hard-coded to rds
			},
		}

		cluster.EdsClusterConfig = &xdsapi.Cluster_EdsClusterConfig{
			ServiceName: key,
			EdsConfig: &core.ConfigSource{
				ConfigSourceSpecifier: apiSource,
			},
		}
	}

	if port.Protocol == model.ProtocolGRPC || port.Protocol == model.ProtocolHTTP2 {
		cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
	}
	return cluster
}

// BuildInboundCluster builds an inbound cluster.
func buildInboundCluster2(port int, protocol model.Protocol, timeout *duration.Duration) *xdsapi.Cluster {
	cluster := &xdsapi.Cluster{
		Name:           fmt.Sprintf("%s%d", "in.", port),
		Type:           xdsapi.Cluster_STATIC,
		ConnectTimeout: convertDuration(timeout),
		LbPolicy:       xdsapi.Cluster_ROUND_ROBIN,
	}
	a := core.Address_SocketAddress{
		SocketAddress: &core.SocketAddress{
			Address:       "127.0.0.1",
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(port)},
		},
	}
	cluster.Hosts = []*core.Address{
		{
			Address: &a,
		},
	}
	if protocol == model.ProtocolGRPC || protocol == model.ProtocolHTTP2 {
		cluster.Http2ProtocolOptions = &core.Http2ProtocolOptions{}
	}
	return cluster
}

// BuildOriginalDSTCluster builds a DST cluster.
func buildOriginalDSTCluster2(name string, timeout *duration.Duration) *xdsapi.Cluster {
	return &xdsapi.Cluster{
		Name:           v1.TruncateClusterName(OutboundClusterPrefix + name),
		Type:           xdsapi.Cluster_ORIGINAL_DST,
		ConnectTimeout: convertDuration(timeout),
		LbPolicy:       xdsapi.Cluster_ORIGINAL_DST_LB,
	}
}

// convertDuration converts to golang duration and logs errors
func convertDuration(d *duration.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := ptypes.Duration(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}
