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

package client_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/tcp_proxy/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"google.golang.org/grpc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer"
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
  - name: "outbound|||svc.ns3"
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.ServerProxyPort}}
  - name: "inbound|||backend"
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.BackendPort}}
  - name: "outbound|9091||mixer_server"
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.MixerPort}}
`

const (
	checkAttributesOkOutbound = `
{
	"connection.id": "*",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "context.protocol": "tcp",
  "context.reporter.kind": "outbound",
  "context.reporter.uid": "kubernetes://pod2.ns2",
  "context.time": "*",
  "destination.service.host": "svc.ns3",
  "destination.service.name": "svc",
  "destination.service.namespace": "ns3",
  "destination.service.uid": "istio://ns3/services/svc",
  "source.namespace": "ns2",
  "source.uid": "kubernetes://pod2.ns2",
  "source.ip": "[127 0 0 1]"
}
`
	checkAttributesOkInbound = `
{
	"connection.id": "*",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "context.protocol": "tcp",
  "context.reporter.kind": "inbound",
  "context.reporter.uid": "kubernetes://pod1.ns1",
  "context.time": "*",
  "destination.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 127 0 0 1]",
  "destination.port": "*",
  "destination.namespace": "ns1",
  "destination.uid": "kubernetes://pod1.ns1",
  "source.ip": "[127 0 0 1]"
}
`
)

func TestPilotPluginTCP(t *testing.T) {
	s := env.NewTestSetup(env.PilotPluginTCPTest, t)
	s.EnvoyTemplate = envoyConf
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Ports().DiscoveryPort))
	if err != nil {
		t.Fatal(err)
	}

	snapshots := cache.NewSnapshotCache(true, mock{}, nil)
	snapshots.SetSnapshot(id, makeSnapshot(s, t))
	server := xds.NewServer(snapshots, nil)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	s.WaitEnvoyReady()

	// Issues a GET echo request with 0 size body
	if _, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)); err != nil {
		t.Errorf("Failed in request: %v", err)
	}
	s.VerifyCheck("tcp-outbound", checkAttributesOkOutbound)
	s.VerifyCheck("tcp-inbound", checkAttributesOkInbound)

	// TODO: verify reports
}

type mock struct{}

func (mock) ID(*core.Node) string {
	return id
}
func (mock) GetProxyServiceInstances(_ *model.Proxy) ([]*model.ServiceInstance, error) {
	return nil, nil
}
func (mock) GetService(_ model.Hostname) (*model.Service, error) { return nil, nil }
func (mock) InstancesByPort(_ model.Hostname, _ int, _ model.LabelsCollection) ([]*model.ServiceInstance, error) {
	return nil, nil
}
func (mock) ManagementPorts(_ string) model.PortList          { return nil }
func (mock) Services() ([]*model.Service, error)              { return nil, nil }
func (mock) WorkloadHealthCheckInfo(_ string) model.ProbeList { return nil }

const (
	id = "id"
)

var (
	svc = model.Service{
		Hostname: "svc.ns3",
		Attributes: model.ServiceAttributes{
			Name:      "svc",
			Namespace: "ns3",
			UID:       "istio://ns3/services/svc",
		},
	}
	mesh = &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer:            "mixer_server:9091",
			MixerReportServer:           "mixer_server:9091",
			EnableClientSidePolicyCheck: true,
		},
		ServiceDiscovery: mock{},
	}
	pushContext = model.PushContext{
		ServiceByHostname: map[model.Hostname]*model.Service{
			model.Hostname("svc.ns3"): &svc,
		},
	}
	serverParams = plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolTCP,
		Env:              mesh,
		Node: &model.Proxy{
			ID:   "pod1.ns1",
			Type: model.Sidecar,
			Metadata: map[string]string{
				"ISTIO_PROXY_VERSION": "1.1",
			},
		},
		ServiceInstance: &model.ServiceInstance{Service: &svc},
		Push:            &pushContext,
	}
	clientParams = plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolTCP,
		Env:              mesh,
		Node: &model.Proxy{
			ID:   "pod2.ns2",
			Type: model.Sidecar,
			Metadata: map[string]string{
				"ISTIO_PROXY_VERSION": "1.1",
			},
		},
		Service: &svc,
		Push:    &pushContext,
	}
)

func makeListener(port uint16, cluster string) *v2.Listener {
	return &v2.Listener{
		Name: cluster,
		Address: core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
			Address:       "127.0.0.1",
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(port)}}}},
		FilterChains: []listener.FilterChain{{Filters: []listener.Filter{{
			Name: util.TCPProxy,
			Config: pilotutil.MessageToStruct(&tcp_proxy.TcpProxy{
				StatPrefix:       "tcp",
				ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: cluster},
			}),
		}}}},
	}
}

func makeSnapshot(s *env.TestSetup, t *testing.T) cache.Snapshot {
	clientListener := makeListener(s.Ports().ClientProxyPort, "outbound|||svc.ns3")
	serverListener := makeListener(s.Ports().ServerProxyPort, "inbound|||backend")

	p := mixer.NewPlugin()

	serverMutable := plugin.MutableObjects{Listener: serverListener, FilterChains: []plugin.FilterChain{{}}}
	if err := p.OnInboundListener(&serverParams, &serverMutable); err != nil {
		t.Error(err)
	}
	serverListener.FilterChains[0].Filters = append(serverMutable.FilterChains[0].TCP, serverListener.FilterChains[0].Filters...)

	clientMutable := plugin.MutableObjects{Listener: clientListener, FilterChains: []plugin.FilterChain{{}}}
	if err := p.OnOutboundListener(&clientParams, &clientMutable); err != nil {
		t.Error(err)
	}
	clientListener.FilterChains[0].Filters = append(clientMutable.FilterChains[0].TCP, clientListener.FilterChains[0].Filters...)

	return cache.Snapshot{
		Listeners: cache.NewResources("tcp", []cache.Resource{clientListener, serverListener}),
	}
}
