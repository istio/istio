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
	"log"
	"net"
	"strconv"
	"testing"
	"time"

	corev2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc"

	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"

	"istio.io/istio/mixer/test/client/env"
	pilotutil "istio.io/istio/pilot/pkg/networking/util"
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
  "context.reporter.uid": "",
  "connection.mtls": false,
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
  "destination.uid": "",
  "destination.namespace": "",
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

func (hasher) ID(*corev2.Node) string {
	return ""
}

func makeListener(s *env.TestSetup, key string) *listener.Listener {
	mxServiceConfig, err := ptypes.MarshalAny(&mccpb.ServiceConfig{
		MixerAttributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"key": {Value: &mpb.Attributes_AttributeValue_StringValue{StringValue: key}},
			},
		}})
	if err != nil {
		panic(err.Error())
	}
	mxConf := pilotutil.MessageToAny(env.GetDefaultHTTPServerConf())

	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &route.RouteConfiguration{
				Name: key,
				VirtualHosts: []*route.VirtualHost{{
					Name:    "backend",
					Domains: []string{"*"},
					Routes: []*route.Route{{
						Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
						Action: &route.Route_Route{Route: &route.RouteAction{
							ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "backend"},
						}},
						TypedPerFilterConfig: map[string]*any.Any{
							"mixer": mxServiceConfig,
						}}}}}}},
		HttpFilters: []*hcm.HttpFilter{{
			Name:       "mixer",
			ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: mxConf},
		}, {
			Name: wellknown.Router,
		}},
	}

	return &listener.Listener{
		Name: strconv.Itoa(int(s.Ports().ServerProxyPort)),
		Address: &core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
			Address:       "127.0.0.1",
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(s.Ports().ServerProxyPort)}}}},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name:       "http",
				ConfigType: &listener.Filter_TypedConfig{pilotutil.MessageToAny(manager)},
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
	server := xds.NewServer(context.Background(), snapshots, nil)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	snapshot := cache.Snapshot{}
	snapshot.Resources[types.Listener] = cache.Resources{Version: strconv.Itoa(count), Items: map[string]types.Resource{
		"backend": makeListener(s, fmt.Sprintf("count%d", count))}}
	if err := snapshots.SetSnapshot("", snapshot); err != nil {
		t.Fatal(err)
	}

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
		snapshot := cache.Snapshot{}
		snapshot.Resources[types.Listener] = cache.Resources{Version: strconv.Itoa(count), Items: map[string]types.Resource{
			"backend": makeListener(s, fmt.Sprintf("count%d", count))}}
		if err := snapshots.SetSnapshot("", snapshot); err != nil {
			t.Fatal(err)
		}

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
