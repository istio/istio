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

package client_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc"

	"istio.io/istio/mixer/test/client/env"
)

const envoyConf = `
admin:
  access_log_path: {{.AccessLogPath}}
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.Ports.AdminPort}}
node:
  id: id
  cluster: unknown
dynamic_resources:
  ads_config:
    api_type: GRPC
    grpc_services:
      envoy_grpc:
        cluster_name: xds
static_resources:
  clusters:
  - name: xds
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.DiscoveryPort}}
  - name: backend
    connect_timeout: 5s
    type: EDS
    eds_cluster_config: { eds_config: { ads: {}}}
  - name: mixer_server
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.MixerPort}}
  listeners:
  - name: server
    address:
      socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.ServerProxyPort}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: AUTO
          stat_prefix: inbound_http
          access_log:
          - name: envoy.file_access_log
            config:
              path: {{.AccessLogPath}}
          http_filters:
          - name: mixer
            config: {{.MfConfig.HTTPServerConf | toJSON }}
          - name: envoy.router
          route_config:
            name: backend
            virtual_hosts:
            - name: backend
              domains: ["*"]
              routes:
              - match:
                  prefix: /
                route:
                  cluster: backend
                  timeout: 0s
                per_filter_config:
                  mixer: {{.MfConfig.PerRouteConf | toJSON }}
  - name: tcp_server
    address:
      socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.TCPProxyPort}}
    filter_chains:
    - filters:
      - name: mixer
        config: {{.MfConfig.TCPServerConf | toJSON }}
      - name: envoy.tcp_proxy
        config:
          stat_prefix: inbound_tcp
          cluster: backend
`

// Report attributes from a good GET request
const reportAttributesOkGet = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "destination.uid": "pod1.ns2",
  "destination.namespace": "",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "check.cache_hit": false,
  "quota.cache_hit": false,
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 0,
  "response.time": "*",
  "response.size": 0,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
     "date": "*",
     "content-length": "0",
     ":status": "200",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": "*"
}
`

type hasher struct{}

func (hasher) ID(*core.Node) string {
	return ""
}

func TestDynamicAttribute(t *testing.T) {
	s := env.NewTestSetup(env.DynamicAttributeTest, t)
	s.EnvoyTemplate = envoyConf
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Ports().DiscoveryPort))
	if err != nil {
		t.Fatal(err)
	}

	snapshots := cache.NewSnapshotCache(false, hasher{}, nil)
	snapshot := cache.Snapshot{}
	snapshot.Resources[types.Endpoint] = cache.Resources{Version: "1", Items: map[string]types.Resource{
		"backend": &v2.ClusterLoadAssignment{
			ClusterName: "backend",
			Endpoints: []*endpoint.LocalityLbEndpoints{{
				LbEndpoints: []*endpoint.LbEndpoint{{
					Metadata: &core.Metadata{
						FilterMetadata: map[string]*structpb.Struct{
							"istio": {
								Fields: map[string]*structpb.Value{
									"uid": {Kind: &structpb.Value_StringValue{StringValue: "pod1.ns2"}},
								},
							},
						},
					},
					HostIdentifier: &endpoint.LbEndpoint_Endpoint{
						Endpoint: &endpoint.Endpoint{
							Address: &core.Address{Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Address:       "127.0.0.1",
									PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(s.Ports().BackendPort)},
								},
							}},
						},
					},
				}},
			}},
		},
	}}
	snapshots.SetSnapshot("", snapshot)
	server := xds.NewServer(context.Background(), snapshots, nil)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// Issues a GET echo request with 0 size body
	if _, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)); err != nil {
		t.Errorf("Failed in request: %v", err)
	}
	s.VerifyReport("http", reportAttributesOkGet)
}
