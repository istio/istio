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
	"sort"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	mongo_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/mongo_proxy/v2"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"

	"istio.io/istio/pilot/pkg/model"
)

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func buildInboundNetworkFilters(instance *model.ServiceInstance) []listener.Filter {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "",
		instance.Service.Hostname, instance.Endpoint.ServicePort)
	config := &tcp_proxy.TcpProxy{
		StatPrefix: fmt.Sprintf("%s|tcp|%d", model.TrafficDirectionInbound, instance.Endpoint.ServicePort.Port),
		Cluster:    clusterName,
	}

	return []listener.Filter{
		{
			Name:   util.TCPProxy,
			Config: messageToStruct(config),
		},
	}

}

// buildOutboundNetworkFilters generates TCP proxy network filter for outbound connections. In addition, it generates
// protocol specific filters (e.g., Mongo filter)
// this function constructs deprecated_v1 routes, until the filter chain match is ready
func buildOutboundNetworkFilters(clusterName string, addresses []string, port *model.Port) []listener.Filter {

	// destination port is unnecessary with use_original_dst since
	// the listener address already contains the port
	route := &tcp_proxy.TcpProxy_DeprecatedV1_TCPRoute{Cluster: clusterName}

	if len(addresses) > 0 {
		sort.Sort(sort.StringSlice(addresses))
		route.DestinationIpList = append(route.DestinationIpList, convertAddressListToCidrList(addresses)...)
	}

	config := &tcp_proxy.TcpProxy{
		StatPrefix: fmt.Sprintf("%s|tcp|%d", model.TrafficDirectionOutbound, port.Port),
		DeprecatedV1: &tcp_proxy.TcpProxy_DeprecatedV1{
			Routes: []*tcp_proxy.TcpProxy_DeprecatedV1_TCPRoute{route},
		},
	}

	tcpFilter := listener.Filter{
		Name:   util.TCPProxy,
		Config: messageToStruct(config),
	}

	filterstack := make([]listener.Filter, 0)
	switch port.Protocol {
	case model.ProtocolMongo:
		filterstack = append(filterstack, buildOutboundMongoFilter())
	}
	filterstack = append(filterstack, tcpFilter)

	return filterstack
}

func buildOutboundMongoFilter() listener.Filter {
	// TODO: add a watcher for /var/lib/istio/mongo/certs
	// if certs are found use, TLS or mTLS clusters for talking to MongoDB.
	// User is responsible for mounting those certs in the pod.
	config := &mongo_proxy.MongoProxy{
		StatPrefix: "mongo",
		// TODO enable faults in mongo
	}

	return listener.Filter{
		Name:   util.MongoProxy,
		Config: messageToStruct(config),
	}
}
