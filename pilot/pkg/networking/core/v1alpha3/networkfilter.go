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

package v1alpha3

import (
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	mongo "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/mongo_proxy/v3"
	mysql "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/mysql_proxy/v3"
	redis "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/redis_proxy/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	thrift "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/thrift_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
)

// redisOpTimeout is the default operation timeout for the Redis proxy filter.
var redisOpTimeout = 5 * time.Second

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func buildInboundNetworkFilters(push *model.PushContext, instance *model.ServiceInstance, node *model.Proxy) []*listener.Filter {
	clusterName := model.BuildInboundSubsetKey(int(instance.Endpoint.EndpointPort))
	statPrefix := clusterName
	// If stat name is configured, build the stat prefix from configured pattern.
	if len(push.Mesh.InboundClusterStatName) != 0 {
		statPrefix = util.BuildStatPrefix(push.Mesh.InboundClusterStatName, string(instance.Service.Hostname), "", instance.ServicePort, instance.Service.Attributes)
	}
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: clusterName},
	}
	tcpFilter := setAccessLogAndBuildTCPFilter(push, tcpProxy, node)
	return buildNetworkFiltersStack(instance.ServicePort, tcpFilter, statPrefix, clusterName)
}

// setAccessLogAndBuildTCPFilter sets the AccessLog configuration in the given
// TcpProxy instance and builds a TCP filter out of it.
func setAccessLogAndBuildTCPFilter(push *model.PushContext, config *tcp.TcpProxy, node *model.Proxy) *listener.Filter {
	accessLogBuilder.setTCPAccessLog(push.Mesh, config, node)

	tcpFilter := &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(config)},
	}
	return tcpFilter
}

// buildOutboundNetworkFiltersWithSingleDestination takes a single cluster name
// and builds a stack of network filters.
func buildOutboundNetworkFiltersWithSingleDestination(push *model.PushContext, node *model.Proxy,
	statPrefix, clusterName string, port *model.Port) []*listener.Filter {
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: clusterName},
		// TODO: Need to set other fields such as Idle timeouts
	}

	idleTimeout, err := time.ParseDuration(node.Metadata.IdleTimeout)
	if err == nil {
		tcpProxy.IdleTimeout = ptypes.DurationProto(idleTimeout)
	}

	tcpFilter := setAccessLogAndBuildTCPFilter(push, tcpProxy, node)
	return buildNetworkFiltersStack(port, tcpFilter, statPrefix, clusterName)
}

// buildOutboundNetworkFiltersWithWeightedClusters takes a set of weighted
// destination routes and builds a stack of network filters.
func buildOutboundNetworkFiltersWithWeightedClusters(node *model.Proxy, routes []*networking.RouteDestination,
	push *model.PushContext, port *model.Port, configMeta config.Meta) []*listener.Filter {
	statPrefix := configMeta.Name + "." + configMeta.Namespace
	clusterSpecifier := &tcp.TcpProxy_WeightedClusters{
		WeightedClusters: &tcp.TcpProxy_WeightedCluster{},
	}

	proxyConfig := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: clusterSpecifier,
		// TODO: Need to set other fields such as Idle timeouts
	}

	idleTimeout, err := time.ParseDuration(node.Metadata.IdleTimeout)
	if err == nil {
		proxyConfig.IdleTimeout = ptypes.DurationProto(idleTimeout)
	}

	for _, route := range routes {
		service := push.ServiceForHostname(node, host.Name(route.Destination.Host))
		if route.Weight > 0 {
			clusterName := istio_route.GetDestinationCluster(route.Destination, service, port.Port)
			clusterSpecifier.WeightedClusters.Clusters = append(clusterSpecifier.WeightedClusters.Clusters, &tcp.TcpProxy_WeightedCluster_ClusterWeight{
				Name:   clusterName,
				Weight: uint32(route.Weight),
			})
		}
	}

	// TODO: Need to handle multiple cluster names for Redis
	clusterName := clusterSpecifier.WeightedClusters.Clusters[0].Name
	tcpFilter := setAccessLogAndBuildTCPFilter(push, proxyConfig, node)
	return buildNetworkFiltersStack(port, tcpFilter, statPrefix, clusterName)
}

// buildNetworkFiltersStack builds a slice of network filters based on
// the protocol in use and the given TCP filter instance.
func buildNetworkFiltersStack(port *model.Port, tcpFilter *listener.Filter, statPrefix string, clusterName string) []*listener.Filter {
	filterstack := make([]*listener.Filter, 0)
	switch port.Protocol {
	case protocol.Mongo:
		filterstack = append(filterstack, buildMongoFilter(statPrefix), tcpFilter)
	case protocol.Redis:
		if features.EnableRedisFilter {
			// redis filter has route config, it is a terminating filter, no need append tcp filter.
			filterstack = append(filterstack, buildRedisFilter(statPrefix, clusterName))
		} else {
			filterstack = append(filterstack, tcpFilter)
		}
	case protocol.MySQL:
		if features.EnableMysqlFilter {
			filterstack = append(filterstack, buildMySQLFilter(statPrefix))
		}
		filterstack = append(filterstack, tcpFilter)
	case protocol.Thrift:
		if features.EnableThriftFilter {
			// Thrift filter has route config, it is a terminating filter, no need append tcp filter.
			filterstack = append(filterstack, buildThriftFilter(statPrefix))
		} else {
			filterstack = append(filterstack, tcpFilter)
		}
	default:
		filterstack = append(filterstack, tcpFilter)
	}

	return filterstack
}

// buildOutboundNetworkFilters generates a TCP proxy network filter for outbound
// connections. In addition, it generates protocol specific filters (e.g., Mongo
// filter).
func buildOutboundNetworkFilters(node *model.Proxy,
	routes []*networking.RouteDestination, push *model.PushContext,
	port *model.Port, configMeta config.Meta) []*listener.Filter {
	if len(routes) == 1 {
		service := push.ServiceForHostname(node, host.Name(routes[0].Destination.Host))
		clusterName := istio_route.GetDestinationCluster(routes[0].Destination, service, port.Port)
		statPrefix := clusterName
		// If stat name is configured, build the stat prefix from configured pattern.
		if len(push.Mesh.OutboundClusterStatName) != 0 && service != nil {
			statPrefix = util.BuildStatPrefix(push.Mesh.OutboundClusterStatName, routes[0].Destination.Host,
				routes[0].Destination.Subset, port, service.Attributes)
		}
		return buildOutboundNetworkFiltersWithSingleDestination(push, node, statPrefix, clusterName, port)
	}
	return buildOutboundNetworkFiltersWithWeightedClusters(node, routes, push, port, configMeta)
}

// buildThriftFilter builds an outbound Envoy Thrift filter.
func buildThriftFilter(statPrefix string) *listener.Filter {
	thriftProxy := &thrift.ThriftProxy{
		StatPrefix: statPrefix, // TODO (peter.novotnak@reddit.com) Thrift stats are prefixed with thrift.<statPrefix> by Envoy.
	}

	out := &listener.Filter{
		Name:       wellknown.ThriftProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(thriftProxy)},
	}

	return out
}

// buildMongoFilter builds an outbound Envoy MongoProxy filter.
func buildMongoFilter(statPrefix string) *listener.Filter {
	// TODO: add a watcher for /var/lib/istio/mongo/certs
	// if certs are found use, TLS or mTLS clusters for talking to MongoDB.
	// User is responsible for mounting those certs in the pod.
	mongoProxy := &mongo.MongoProxy{
		StatPrefix: statPrefix, // mongo stats are prefixed with mongo.<statPrefix> by Envoy
		// TODO enable faults in mongo
	}

	out := &listener.Filter{
		Name:       wellknown.MongoProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(mongoProxy)},
	}

	return out
}

// buildOutboundAutoPassthroughFilterStack builds a filter stack with sni_cluster and tcp
// used by auto_passthrough gateway servers
func buildOutboundAutoPassthroughFilterStack(push *model.PushContext, node *model.Proxy, port *model.Port) []*listener.Filter {
	// First build tcp with access logs
	// then add sni_cluster to the front
	tcpProxy := buildOutboundNetworkFiltersWithSingleDestination(push, node, util.BlackHoleCluster, util.BlackHoleCluster, port)
	filterstack := make([]*listener.Filter, 0)
	filterstack = append(filterstack, &listener.Filter{
		Name: util.SniClusterFilter,
	})
	filterstack = append(filterstack, tcpProxy...)

	return filterstack
}

// buildRedisFilter builds an outbound Envoy RedisProxy filter.
// Currently, if multiple clusters are defined, one of them will be picked for
// configuring the Redis proxy.
func buildRedisFilter(statPrefix, clusterName string) *listener.Filter {
	redisProxy := &redis.RedisProxy{
		LatencyInMicros: true,       // redis latency stats are captured in micro seconds which is typically the case.
		StatPrefix:      statPrefix, // redis stats are prefixed with redis.<statPrefix> by Envoy
		Settings: &redis.RedisProxy_ConnPoolSettings{
			OpTimeout: ptypes.DurationProto(redisOpTimeout), // TODO: Make this user configurable
		},
		PrefixRoutes: &redis.RedisProxy_PrefixRoutes{
			CatchAllRoute: &redis.RedisProxy_PrefixRoutes_Route{
				Cluster: clusterName,
			},
		},
	}

	out := &listener.Filter{
		Name:       wellknown.RedisProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(redisProxy)},
	}

	return out
}

// buildMySQLFilter builds an outbound Envoy MySQLProxy filter.
func buildMySQLFilter(statPrefix string) *listener.Filter {
	mySQLProxy := &mysql.MySQLProxy{
		StatPrefix: statPrefix, // MySQL stats are prefixed with mysql.<statPrefix> by Envoy.
	}

	out := &listener.Filter{
		Name:       wellknown.MySQLProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(mySQLProxy)},
	}

	return out
}
