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

	mysql "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/filters/network/mysql_proxy/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	mongo "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/mongo_proxy/v3"
	redis "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/redis_proxy/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	hashpolicy "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/durationpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	istioroute "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/tunnelingconfig"
	"istio.io/istio/pilot/pkg/networking/telemetry"
	"istio.io/istio/pilot/pkg/networking/util"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
)

// redisOpTimeout is the default operation timeout for the Redis proxy filter.
var redisOpTimeout = 5 * time.Second

func buildMetadataExchangeNetworkFilters(class istionetworking.ListenerClass) []*listener.Filter {
	filterstack := make([]*listener.Filter, 0)
	// We add metadata exchange on inbound only; outbound is handled in cluster filter
	if class == istionetworking.ListenerClassSidecarInbound && features.MetadataExchange {
		filterstack = append(filterstack, xdsfilters.TCPListenerMx)
	}

	return filterstack
}

func buildMetadataExchangeNetworkFiltersForTCPIstioMTLSGateway() []*listener.Filter {
	filterstack := make([]*listener.Filter, 0)
	// We add metadata exchange on inbound only; outbound is handled in cluster filter
	if features.MetadataExchange {
		filterstack = append(filterstack, xdsfilters.TCPListenerMx)
	}

	return filterstack
}

func buildMetricsNetworkFilters(push *model.PushContext, proxy *model.Proxy, class istionetworking.ListenerClass) []*listener.Filter {
	return push.Telemetry.TCPFilters(proxy, class)
}

// setAccessLogAndBuildTCPFilter sets the AccessLog configuration in the given
// TcpProxy instance and builds a TCP filter out of it.
func setAccessLogAndBuildTCPFilter(push *model.PushContext, node *model.Proxy, config *tcp.TcpProxy, class istionetworking.ListenerClass) *listener.Filter {
	accessLogBuilder.setTCPAccessLog(push, node, config, class)

	tcpFilter := &listener.Filter{
		Name:       wellknown.TCPProxy,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(config)},
	}
	return tcpFilter
}

// buildOutboundNetworkFiltersWithSingleDestination takes a single cluster name
// and builds a stack of network filters.
func buildOutboundNetworkFiltersWithSingleDestination(push *model.PushContext, node *model.Proxy,
	statPrefix, clusterName, subsetName string, port *model.Port, destinationRule *networking.DestinationRule, applyTunnelingConfig tunnelingconfig.ApplyFunc,
) []*listener.Filter {
	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: clusterName},
	}

	idleTimeout, err := time.ParseDuration(node.Metadata.IdleTimeout)
	if err == nil {
		tcpProxy.IdleTimeout = durationpb.New(idleTimeout)
	}
	maybeSetHashPolicy(destinationRule, tcpProxy, subsetName)
	applyTunnelingConfig(tcpProxy, destinationRule, subsetName)
	class := model.OutboundListenerClass(node.Type)
	tcpFilter := setAccessLogAndBuildTCPFilter(push, node, tcpProxy, class)

	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(class)...)
	filters = append(filters, buildMetricsNetworkFilters(push, node, class)...)
	filters = append(filters, buildNetworkFiltersStack(port.Protocol, tcpFilter, statPrefix, clusterName)...)
	return filters
}

// buildOutboundNetworkFiltersWithWeightedClusters takes a set of weighted
// destination routes and builds a stack of network filters.
func buildOutboundNetworkFiltersWithWeightedClusters(node *model.Proxy, routes []*networking.RouteDestination,
	push *model.PushContext, port *model.Port, configMeta config.Meta, destinationRule *networking.DestinationRule,
) []*listener.Filter {
	statPrefix := configMeta.Name + "." + configMeta.Namespace
	clusterSpecifier := &tcp.TcpProxy_WeightedClusters{
		WeightedClusters: &tcp.TcpProxy_WeightedCluster{},
	}

	tcpProxy := &tcp.TcpProxy{
		StatPrefix:       statPrefix,
		ClusterSpecifier: clusterSpecifier,
	}

	idleTimeout, err := time.ParseDuration(node.Metadata.IdleTimeout)
	if err == nil {
		tcpProxy.IdleTimeout = durationpb.New(idleTimeout)
	}

	for _, route := range routes {
		service := push.ServiceForHostname(node, host.Name(route.Destination.Host))
		if route.Weight > 0 {
			clusterName := istioroute.GetDestinationCluster(route.Destination, service, port.Port)
			clusterSpecifier.WeightedClusters.Clusters = append(clusterSpecifier.WeightedClusters.Clusters, &tcp.TcpProxy_WeightedCluster_ClusterWeight{
				Name:   clusterName,
				Weight: uint32(route.Weight),
			})
		}
	}

	// For weighted clusters set hash policy if any of the upstream destinations have sourceIP.
	maybeSetHashPolicy(destinationRule, tcpProxy, "")
	// In case of weighted clusters, tunneling config for a subset is ignored,
	// because it is set on listener, not on a cluster.
	tunnelingconfig.Apply(tcpProxy, destinationRule, "")

	// TODO: Need to handle multiple cluster names for Redis
	clusterName := clusterSpecifier.WeightedClusters.Clusters[0].Name
	class := model.OutboundListenerClass(node.Type)
	tcpFilter := setAccessLogAndBuildTCPFilter(push, node, tcpProxy, class)

	var filters []*listener.Filter
	filters = append(filters, buildMetadataExchangeNetworkFilters(class)...)
	filters = append(filters, buildMetricsNetworkFilters(push, node, class)...)
	filters = append(filters, buildNetworkFiltersStack(port.Protocol, tcpFilter, statPrefix, clusterName)...)
	return filters
}

func maybeSetHashPolicy(destinationRule *networking.DestinationRule, tcpProxy *tcp.TcpProxy, subsetName string) {
	if destinationRule != nil {
		useSourceIP := destinationRule.GetTrafficPolicy().GetLoadBalancer().GetConsistentHash().GetUseSourceIp()
		for _, subset := range destinationRule.Subsets {
			if subset.Name != subsetName {
				continue
			}
			// If subset has load balancer - see if it is also consistent hash source IP
			if subset.TrafficPolicy != nil && subset.TrafficPolicy.LoadBalancer != nil {
				if subset.TrafficPolicy.LoadBalancer.GetConsistentHash() != nil {
					useSourceIP = subset.TrafficPolicy.LoadBalancer.GetConsistentHash().GetUseSourceIp()
				} else {
					// This means that subset has defined non sourceIP consistent hash load balancer.
					useSourceIP = false
				}
			}
			break
		}
		// If destinationrule has consistent hash source ip set, use it for tcp proxy.
		if useSourceIP {
			tcpProxy.HashPolicy = []*hashpolicy.HashPolicy{{PolicySpecifier: &hashpolicy.HashPolicy_SourceIp_{
				SourceIp: &hashpolicy.HashPolicy_SourceIp{},
			}}}
		}
	}
}

// buildNetworkFiltersStack builds a slice of network filters based on
// the protocol in use and the given TCP filter instance.
func buildNetworkFiltersStack(p protocol.Instance, tcpFilter *listener.Filter, statPrefix string, clusterName string) []*listener.Filter {
	filterstack := make([]*listener.Filter, 0)
	switch p {
	case protocol.Mongo:
		if features.EnableMongoFilter {
			filterstack = append(filterstack, buildMongoFilter(statPrefix), tcpFilter)
		} else {
			filterstack = append(filterstack, tcpFilter)
		}
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
	port *model.Port, configMeta config.Meta,
) []*listener.Filter {
	service := push.ServiceForHostname(node, host.Name(routes[0].Destination.Host))
	var destinationRule *networking.DestinationRule
	if service != nil {
		destinationRule = CastDestinationRule(node.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, node, service.Hostname))
	}
	if len(routes) == 1 {
		clusterName := istioroute.GetDestinationCluster(routes[0].Destination, service, port.Port)
		statPrefix := clusterName
		// If stat name is configured, build the stat prefix from configured pattern.
		if len(push.Mesh.OutboundClusterStatName) != 0 && service != nil {
			statPrefix = telemetry.BuildStatPrefix(push.Mesh.OutboundClusterStatName, routes[0].Destination.Host,
				routes[0].Destination.Subset, port, &service.Attributes)
		}

		return buildOutboundNetworkFiltersWithSingleDestination(
			push, node, statPrefix, clusterName, routes[0].Destination.Subset, port, destinationRule, tunnelingconfig.Apply)
	}
	return buildOutboundNetworkFiltersWithWeightedClusters(node, routes, push, port, configMeta, destinationRule)
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
	tcpProxy := buildOutboundNetworkFiltersWithSingleDestination(push, node, util.BlackHoleCluster, util.BlackHoleCluster,
		"", port, nil, tunnelingconfig.Skip)
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
			OpTimeout: durationpb.New(redisOpTimeout),
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
