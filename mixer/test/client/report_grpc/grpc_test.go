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

package client_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	"istio.io/istio/mixer/test/client/env"
)

const (
	// Report attributes from a GRPC request
	reportAttributesGRPC = `
{
	"connection.mtls": false,
	"context.protocol": "grpc",
	"context.proxy_error_code": "-",
	"context.reporter.uid": "",
	"destination.ip": "[127 0 0 1]",
	"destination.namespace": "",
	"destination.port": "*",
	"destination.uid": "",
	"request.host": "*",
	"request.path": "/envoy.service.discovery.v2.AggregatedDiscoveryService/StreamAggregatedResources",
	"request.time": "*",
	"request.useragent": "*",
	"request.method": "POST",
	"request.scheme": "http",
	"request.url_path": "/envoy.service.discovery.v2.AggregatedDiscoveryService/StreamAggregatedResources",
	"request.headers": {
		 ":method": "POST",
		 ":path": "/envoy.service.discovery.v2.AggregatedDiscoveryService/StreamAggregatedResources",
		 ":authority": "*",
		 "x-forwarded-proto": "http",
		 "x-istio-attributes": "-",
		 "x-request-id": "*"
	},
	"request.size": "*",
	"request.total_size": "*",
	"request.grpc_message_count": "%d",
	"origin.ip": "[127 0 0 1]",
	"response.time": "*",
	"response.size": "*",
	"response.duration": "*",
	"response.code": 200,
	"response.headers": {
		 "date": "*",
		 ":status": "200",
		 "server": "envoy"
	},
	"response.grpc_message": "",
	"response.grpc_message_count": "%d",
	"response.grpc_status": "0",
	"response.total_size": "*"
}`

	envoyConfig = `
admin:
  access_log_path: {{.AccessLogPath}}
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.Ports.AdminPort}}
static_resources:
  clusters:
  - name: backend
    connect_timeout: 5s
    http2_protocol_options: {}
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.BackendPort}}
  - name: mixer
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
          stat_prefix: inbound
          http_filters:
          - name: mixer
            config:
              default_destination_service: default
              service_configs:
                default:
                  disable_check_calls: true
              transport:
                report_cluster: mixer
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
                  timeout: 0s`

	// number of messages to send on the client
	num = 1000
)

func TestGRPCAttributes(t *testing.T) {
	s := env.NewTestSetup(env.GRPCTest, t)

	// custom gRPC server
	s.SetNoBackend(true)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Ports().BackendPort))
	if err != nil {
		t.Fatal(err)
	}
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, testServer{})
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	// start envoy
	s.EnvoyTemplate = envoyConfig
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	makeRequests(num, s.Ports().ServerProxyPort, t)
	s.VerifyReport("grpc", fmt.Sprintf(reportAttributesGRPC, num, 2*num))
}

type testServer struct{}

func (testServer) StreamAggregatedResources(s discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	d := 0
	for {
		// read all inputs, respond to each twice
		_, err := s.Recv()
		if err != nil {
			return nil
		}
		d = d + 1
		s.Send(&v2.DiscoveryResponse{
			VersionInfo: fmt.Sprintf("response%d", d),
			Nonce:       strings.Repeat("@", d),
		})
		s.Send(&v2.DiscoveryResponse{})
	}
}
func (testServer) IncrementalAggregatedResources(discovery.AggregatedDiscoveryService_IncrementalAggregatedResourcesServer) error {
	return nil
}

func makeRequests(num int, port uint16, t *testing.T) {
	// custom gRPC client
	conn, err := grpc.Dial(fmt.Sprintf(":%d", port), grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	client := discovery.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := client.StreamAggregatedResources(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	wait := make(chan struct{})
	go func() {
		for {
			_, err := stream.Recv()
			if err != nil {
				close(wait)
				return
			}
		}
	}()

	stream.Send(&v2.DiscoveryRequest{})
	for i := 0; i < num-2; i++ {
		stream.Send(&v2.DiscoveryRequest{
			VersionInfo: fmt.Sprintf("version%d", i),
			Node: &core.Node{
				Id: strings.Repeat("#", i+1),
			},
		})
	}
	stream.Send(&v2.DiscoveryRequest{})
	stream.CloseSend()
	<-wait
}
