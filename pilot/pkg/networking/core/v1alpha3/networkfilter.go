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
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	mongo_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/mongo_proxy/v2"
	mysql_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/mysql_proxy/v1alpha1"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/util"
)

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func buildInboundNetworkFilters(env *model.Environment, node *model.Proxy, instance *model.ServiceInstance) []listener.Filter {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Name,
		instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
	config := &tcp_proxy.TcpProxy{
		StatPrefix:       clusterName,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: clusterName},
	}
	tcpFilter := setAccessLogAndBuildTCPFilter(env, node, config)
	return buildNetworkFiltersStack(node, instance.Endpoint.ServicePort, tcpFilter, clusterName)
}

// setAccessLogAndBuildTCPFilter sets the AccessLog configuration in the given
// TcpProxy instance and builds a TCP filter out of it.
func setAccessLogAndBuildTCPFilter(env *model.Environment, node *model.Proxy, config *tcp_proxy.TcpProxy) *listener.Filter {
	if env.Mesh.AccessLogFile != "" {
		fl := &fileaccesslog.FileAccessLog{
			Path: env.Mesh.AccessLogFile,
		}

		acc := &accesslog.AccessLog{
			Name: xdsutil.FileAccessLog,
		}
		if util.IsProxyVersionGE11(node) {
			buildAccessLog(fl, env)
		}
		if util.IsXDSMarshalingToAnyEnabled(node) {
			acc.ConfigType = &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)}
		} else {
			acc.ConfigType = &accesslog.AccessLog_Config{Config: util.MessageToStruct(fl)}
		}

		config.AccessLog = []*accesslog.AccessLog{acc}

	}

	tcpFilter := &listener.Filter{
		Name: xdsutil.TCPProxy,
	}
	if util.IsXDSMarshalingToAnyEnabled(node) {
		tcpFilter.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		tcpFilter.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(config)}
	}
	return tcpFilter
}

// buildOutboundNetworkFiltersWithSingleDestination takes a single cluster name
// and builds a stack of network filters.
func buildOutboundNetworkFiltersWithSingleDestination(env *model.Environment, node *model.Proxy,
	clusterName string, port *model.Port) []listener.Filter {

	config := &tcp_proxy.TcpProxy{
		StatPrefix:       clusterName,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: clusterName},
		// TODO: Need to set other fields such as Idle timeouts
	}

	idleTimeout, err := time.ParseDuration(node.Metadata[model.NodeMetadataIdleTimeout])
	if idleTimeout > 0 && err == nil {
		config.IdleTimeout = &idleTimeout
	}

	tcpFilter := setAccessLogAndBuildTCPFilter(env, node, config)
	return buildNetworkFiltersStack(node, port, tcpFilter, clusterName)
}

// buildOutboundNetworkFiltersWithWeightedClusters takes a set of weighted
// destination routes and builds a stack of network filters.
func buildOutboundNetworkFiltersWithWeightedClusters(env *model.Environment, node *model.Proxy, routes []*networking.RouteDestination,
	push *model.PushContext, port *model.Port, config model.ConfigMeta) []listener.Filter {

	statPrefix := fmt.Sprintf("%s.%s", config.Name, config.Namespace)
	clusterSpecifier := &tcp_proxy.TcpProxy_WeightedClusters{
		WeightedClusters: &tcp_proxy.TcpProxy_WeightedCluster{},
	}

	proxyConfig := &tcp_proxy.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: clusterSpecifier,
		// TODO: Need to set other fields such as Idle timeouts
	}

	idleTimeout, err := time.ParseDuration(node.Metadata[model.NodeMetadataIdleTimeout])
	if idleTimeout > 0 && err == nil {
		proxyConfig.IdleTimeout = &idleTimeout
	}

	for _, route := range routes {
		if route.Weight > 0 {
			clusterName := istio_route.GetDestinationCluster(route.Destination, push.ServiceByHostname[model.Hostname(route.Destination.Host)], port.Port)
			clusterSpecifier.WeightedClusters.Clusters = append(clusterSpecifier.WeightedClusters.Clusters, &tcp_proxy.TcpProxy_WeightedCluster_ClusterWeight{
				Name:   clusterName,
				Weight: uint32(route.Weight),
			})
		}
	}

	tcpFilter := setAccessLogAndBuildTCPFilter(env, node, proxyConfig)
	return buildNetworkFiltersStack(node, port, tcpFilter, statPrefix)
}

// buildNetworkFiltersStack builds a slice of network filters based on
// the protocol in use and the given TCP filter instance.
func buildNetworkFiltersStack(node *model.Proxy, port *model.Port, tcpFilter *listener.Filter, statPrefix string) []listener.Filter {
	filterstack := make([]listener.Filter, 0)
	switch port.Protocol {
	case model.ProtocolMongo:
		filterstack = append(filterstack, buildMongoFilter(statPrefix, util.IsProxyVersionGE11(node)))
		filterstack = append(filterstack, *tcpFilter)
	case model.ProtocolMySQL:
		if util.IsProxyVersionGE11(node) {
			filterstack = append(filterstack, buildMySQLFilter(statPrefix, util.IsProxyVersionGE11(node)))
		}
		filterstack = append(filterstack, *tcpFilter)
	default:
		filterstack = append(filterstack, *tcpFilter)
	}
	return filterstack
}

// buildOutboundNetworkFilters generates a TCP proxy network filter for outbound
// connections. In addition, it generates protocol specific filters (e.g., Mongo
// filter).
func buildOutboundNetworkFilters(env *model.Environment, node *model.Proxy,
	routes []*networking.RouteDestination, push *model.PushContext,
	port *model.Port, config model.ConfigMeta) []listener.Filter {

	if !util.IsProxyVersionGE11(node) || len(routes) == 1 {
		service := push.ServiceByHostname[model.Hostname(routes[0].Destination.Host)]
		clusterName := istio_route.GetDestinationCluster(routes[0].Destination, service, port.Port)
		return buildOutboundNetworkFiltersWithSingleDestination(env, node, clusterName, port)
	}
	return buildOutboundNetworkFiltersWithWeightedClusters(env, node, routes, push, port, config)
}

// buildMongoFilter builds an outbound Envoy MongoProxy filter.
func buildMongoFilter(statPrefix string, isXDSMarshalingToAnyEnabled bool) listener.Filter {
	// TODO: add a watcher for /var/lib/istio/mongo/certs
	// if certs are found use, TLS or mTLS clusters for talking to MongoDB.
	// User is responsible for mounting those certs in the pod.
	config := &mongo_proxy.MongoProxy{
		StatPrefix: statPrefix, // mongo stats are prefixed with mongo.<statPrefix> by Envoy
		// TODO enable faults in mongo
	}

	out := listener.Filter{
		Name: xdsutil.MongoProxy,
	}
	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		out.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(config)}
	}

	return out
}

// buildOutboundAutoPassthroughFilterStack builds a filter stack with sni_cluster and tcp_proxy
// used by auto_passthrough gateway servers
func buildOutboundAutoPassthroughFilterStack(env *model.Environment, node *model.Proxy, port *model.Port) []listener.Filter {
	// First build tcp_proxy with access logs
	// then add sni_cluster to the front
	tcpProxy := buildOutboundNetworkFiltersWithSingleDestination(env, node, util.BlackHoleCluster, port)
	filterstack := make([]listener.Filter, 0)
	filterstack = append(filterstack, listener.Filter{
		Name: util.SniClusterFilter,
	})
	filterstack = append(filterstack, tcpProxy...)

	return filterstack
}

// buildMySQLFilter builds an outbound Envoy MySQLProxy filter.
func buildMySQLFilter(statPrefix string, isXDSMarshalingToAnyEnabled bool) listener.Filter {
	config := &mysql_proxy.MySQLProxy{
		StatPrefix: statPrefix, // MySQL stats are prefixed with mysql.<statPrefix> by Envoy.
	}

	out := listener.Filter{
		Name: xdsutil.MySQLProxy,
	}

	if isXDSMarshalingToAnyEnabled {
		out.ConfigType = &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		out.ConfigType = &listener.Filter_Config{Config: util.MessageToStruct(config)}
	}

	return out
}
