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
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	mongo_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/mongo_proxy/v2"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

// buildInboundNetworkFilters generates a TCP proxy network filter on the inbound path
func buildInboundNetworkFilters(env *model.Environment, instance *model.ServiceInstance) []listener.Filter {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "", instance.Service.Hostname, instance.Endpoint.ServicePort.Port)
	config := &tcp_proxy.TcpProxy{
		StatPrefix:       clusterName,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: clusterName},
	}

	if env.Mesh.AccessLogFile != "" {
		fl := &fileaccesslog.FileAccessLog{
			Path:   env.Mesh.AccessLogFile,
			Format: EnvoyTCPLogFormat,
		}
		config.AccessLog = []*accesslog.AccessLog{
			{
				Config: util.MessageToStruct(fl),
				Name:   xdsutil.FileAccessLog,
			},
		}
	}

	return []listener.Filter{
		{
			Name:   xdsutil.TCPProxy,
			Config: util.MessageToStruct(config),
		},
	}
}

// buildOutboundNetworkFilters generates TCP proxy network filter for outbound connections. In addition, it generates
// protocol specific filters (e.g., Mongo filter)
// this function constructs deprecated_v1 routes, until the filter chain match is ready
func buildOutboundNetworkFilters(env *model.Environment, node *model.Proxy, clusterName string,
	port *model.Port) []listener.Filter {

	// construct TCP proxy using v2 config
	config := &tcp_proxy.TcpProxy{
		StatPrefix:       clusterName,
		ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: clusterName},
		// TODO: Need to set other fields such as Idle timeouts
	}

	if env.Mesh.AccessLogFile != "" {
		fl := &fileaccesslog.FileAccessLog{
			Path:   env.Mesh.AccessLogFile,
			Format: EnvoyTCPLogFormat,
		}
		config.AccessLog = []*accesslog.AccessLog{
			{
				Config: util.MessageToStruct(fl),
				Name:   xdsutil.FileAccessLog,
			},
		}
	}

	tcpFilter := &listener.Filter{
		Name:   xdsutil.TCPProxy,
		Config: util.MessageToStruct(config),
	}

	filterstack := make([]listener.Filter, 0)
	switch port.Protocol {
	case model.ProtocolMongo:
		filterstack = append(filterstack, buildOutboundMongoFilter(clusterName))
	}
	filterstack = append(filterstack, *tcpFilter)

	return filterstack
}

func buildOutboundMongoFilter(statPrefix string) listener.Filter {
	// TODO: add a watcher for /var/lib/istio/mongo/certs
	// if certs are found use, TLS or mTLS clusters for talking to MongoDB.
	// User is responsible for mounting those certs in the pod.
	config := &mongo_proxy.MongoProxy{
		StatPrefix: statPrefix, // mongo stats are prefixed with mongo.<statPrefix> by Envoy
		// TODO enable faults in mongo
	}

	return listener.Filter{
		Name:   xdsutil.MongoProxy,
		Config: util.MessageToStruct(config),
	}
}
