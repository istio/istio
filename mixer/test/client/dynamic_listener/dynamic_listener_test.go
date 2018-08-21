// Copyright 2018 Istio Authors. All Rights Reserved.
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
	"fmt"
	"log"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
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
  lds_config: { ads: {} }
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
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.BackendPort}}
  - name: mixer_server
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.MixerPort}}
`

// Check attributes from a good GET request
const checkAttributesOkGet = `
{
  "connection.mtls": false,
  "connection.requested_server_name": "",
  "origin.ip": "[127 0 0 1]",
  "context.protocol": "http",
	"key": "count%s",
  "mesh1.ip": "[1 1 1 1]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "target.namespace": "XYZ222",
  "target.uid": "POD222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-request-id": "*"
  }
}
`

type hasher struct{}

func (hasher) ID(*core.Node) string {
	return ""
}

func makeListener(s *env.TestSetup, key string) *v2.Listener {
	mxServiceConfig, err := util.MessageToStruct(&mccpb.ServiceConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"key": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: key}},
			},
		}})
	if err != nil {
		panic(err)
	}
	mxConf, err := util.MessageToStruct(env.GetDefaultHTTPServerConf())
	if err != nil {
		panic(err)
	}

	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &v2.RouteConfiguration{
				Name: key,
				VirtualHosts: []route.VirtualHost{{
					Name:    "backend",
					Domains: []string{"*"},
					Routes: []route.Route{{
						Match: route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
						Action: &route.Route_Route{Route: &route.RouteAction{
							ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "backend"},
						}},
						PerFilterConfig: map[string]*types.Struct{
							"mixer": mxServiceConfig,
						}}}}}}},
		HttpFilters: []*hcm.HttpFilter{{
			Name:   "mixer",
			Config: mxConf,
		}, {
			Name: util.Router,
		}},
	}

	pbst, err := util.MessageToStruct(manager)
	if err != nil {
		panic(err)
	}

	return &v2.Listener{
		Name: strconv.Itoa(int(s.Ports().ServerProxyPort)),
		Address: core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
			Address:       "127.0.0.1",
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(s.Ports().ServerProxyPort)}}}},
		FilterChains: []listener.FilterChain{{
			Filters: []listener.Filter{{
				Name:   util.HTTPConnectionManager,
				Config: pbst,
			}},
		}},
	}
}

func TestDynamicListener(t *testing.T) {
	s := env.NewTestSetup(env.DynamicListenerTest, t)
	s.EnvoyTemplate = envoyConf
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Ports().DiscoveryPort))
	if err != nil {
		t.Fatal(err)
	}

	snapshots := cache.NewSnapshotCache(false, hasher{}, nil)

	count := 0
	server := xds.NewServer(snapshots, nil)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	snapshots.SetSnapshot("", cache.Snapshot{
		Listeners: cache.Resources{Version: strconv.Itoa(count), Items: map[string]cache.Resource{
			"backend": makeListener(s, fmt.Sprintf("count%d", count))}}})

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	for ; count < 2; count++ {
		log.Printf("iteration %d", count)
		snapshots.SetSnapshot("", cache.Snapshot{
			Listeners: cache.Resources{Version: strconv.Itoa(count), Items: map[string]cache.Resource{
				"backend": makeListener(s, fmt.Sprintf("count%d", count))}}})

		// wait a bit for config to propagate and old listener to drain
		time.Sleep(2 * time.Second)
		s.WaitEnvoyReady()

		// Issues a GET echo request with 0 size body
		if _, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)); err != nil {
			t.Errorf("Failed in request: %v", err)
		}
		s.VerifyCheck("http", fmt.Sprintf(checkAttributesOkGet, strconv.Itoa(count)))
	}
}
