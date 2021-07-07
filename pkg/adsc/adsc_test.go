// Copyright 2020 Istio Authors
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

package adsc

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"sync"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/testing/protocmp"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
)

type testAdscRunServer struct{}

var StreamHandler func(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error

func (t *testAdscRunServer) StreamAggregatedResources(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return StreamHandler(stream)
}

func (t *testAdscRunServer) DeltaAggregatedResources(xdsapi.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return nil
}

func TestADSC_Run(t *testing.T) {
	tests := []struct {
		desc                 string
		inAdsc               *ADSC
		streamHandler        func(server xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
		expectedADSResources *ADSC
	}{
		{
			desc: "stream-no-resources",
			inAdsc: &ADSC{
				Received:   make(map[string]*xdsapi.DiscoveryResponse),
				Updates:    make(chan string),
				XDSUpdates: make(chan *xdsapi.DiscoveryResponse),
				RecvWg:     sync.WaitGroup{},
				cfg:        &Config{},
			},
			streamHandler: func(server xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*xdsapi.DiscoveryResponse{},
			},
		},
		{
			desc: "stream-2-unnamed-resources",
			inAdsc: &ADSC{
				Received:    make(map[string]*xdsapi.DiscoveryResponse),
				Updates:     make(chan string),
				XDSUpdates:  make(chan *xdsapi.DiscoveryResponse),
				RecvWg:      sync.WaitGroup{},
				cfg:         &Config{},
				VersionInfo: map[string]string{},
			},
			streamHandler: func(stream xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
				_ = stream.Send(&xdsapi.DiscoveryResponse{
					TypeUrl: "foo",
				})
				_ = stream.Send(&xdsapi.DiscoveryResponse{
					TypeUrl: "bar",
				})
				return nil
			},
			expectedADSResources: &ADSC{
				Received: map[string]*xdsapi.DiscoveryResponse{
					"foo": {
						TypeUrl: "foo",
					},
					"bar": {
						TypeUrl: "bar",
					},
				},
			},
		},
		// todo tests for listeners, clusters, eds, and routes, not sure how to do this.
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			StreamHandler = tt.streamHandler
			l, err := net.Listen("tcp", ":0")
			if err != nil {
				t.Errorf("Unable to listen with tcp err %v", err)
				return
			}
			tt.inAdsc.url = l.Addr().String()
			xds := grpc.NewServer()
			xdsapi.RegisterAggregatedDiscoveryServiceServer(xds, new(testAdscRunServer))
			go func() {
				err = xds.Serve(l)
				if err != nil {
					log.Println(err)
				}
			}()
			defer xds.GracefulStop()
			if err != nil {
				t.Errorf("Could not start serving ads server %v", err)
				return
			}

			if err := tt.inAdsc.Dial(); err != nil {
				t.Errorf("Dial error: %v", err)
				return
			}
			if err := tt.inAdsc.Run(); err != nil {
				t.Errorf("ADSC: failed running %v", err)
				return
			}
			tt.inAdsc.RecvWg.Wait()
			if !cmp.Equal(tt.inAdsc.Received, tt.expectedADSResources.Received, protocmp.Transform()) {
				t.Errorf("%s: expected recv %v got %v", tt.desc, tt.expectedADSResources.Received, tt.inAdsc.Received)
			}
		})
	}
}

func TestADSC_Save(t *testing.T) {
	tests := []struct {
		desc         string
		expectedJSON map[string]string
		adsc         *ADSC
		err          error
	}{
		{
			desc: "empty",
			expectedJSON: map[string]string{
				"_lds_tcp":  `{}`,
				"_lds_http": `{}`,
				"_rds":      `{}`,
				"_eds":      `{}`,
				"_ecds":     `{}`,
				"_cds":      `{}`,
			},
			err: nil,
			adsc: &ADSC{
				tcpListeners:  map[string]*listener.Listener{},
				httpListeners: map[string]*listener.Listener{},
				routes:        map[string]*route.RouteConfiguration{},
				edsClusters:   map[string]*cluster.Cluster{},
				clusters:      map[string]*cluster.Cluster{},
				eds:           map[string]*endpoint.ClusterLoadAssignment{},
			},
		},
		{
			desc: "populated",
			err:  nil,
			expectedJSON: map[string]string{
				"_lds_tcp": `{"listener-1":{
  "name": "bar",
  "address": null,
  "stat_prefix": "",
  "filter_chains": [
  ],
  "use_original_dst": null,
  "default_filter_chain": null,
  "per_connection_buffer_limit_bytes": null,
  "metadata": null,
  "deprecated_v1": null,
  "drain_type": 0,
  "listener_filters": [
  ],
  "listener_filters_timeout": null,
  "continue_on_listener_filters_timeout": false,
  "transparent": null,
  "freebind": null,
  "socket_options": [
  ],
  "tcp_fast_open_queue_length": null,
  "traffic_direction": 0,
  "udp_listener_config": null,
  "api_listener": null,
  "connection_balance_config": null,
  "reuse_port": false,
  "access_log": [
  ],
  "tcp_backlog_size": null,
  "bind_to_port": null
},"listener-2":{
  "name": "mar",
  "address": null,
  "stat_prefix": "",
  "filter_chains": [
  ],
  "use_original_dst": null,
  "default_filter_chain": null,
  "per_connection_buffer_limit_bytes": null,
  "metadata": null,
  "deprecated_v1": null,
  "drain_type": 0,
  "listener_filters": [
  ],
  "listener_filters_timeout": null,
  "continue_on_listener_filters_timeout": false,
  "transparent": null,
  "freebind": null,
  "socket_options": [
  ],
  "tcp_fast_open_queue_length": null,
  "traffic_direction": 0,
  "udp_listener_config": null,
  "api_listener": null,
  "connection_balance_config": null,
  "reuse_port": false,
  "access_log": [
  ],
  "tcp_backlog_size": null,
  "bind_to_port": null
}}`,
				"_lds_http": `{"http-list-1":{
  "name": "bar",
  "address": null,
  "stat_prefix": "",
  "filter_chains": [
  ],
  "use_original_dst": null,
  "default_filter_chain": null,
  "per_connection_buffer_limit_bytes": null,
  "metadata": null,
  "deprecated_v1": null,
  "drain_type": 0,
  "listener_filters": [
  ],
  "listener_filters_timeout": null,
  "continue_on_listener_filters_timeout": false,
  "transparent": null,
  "freebind": null,
  "socket_options": [
  ],
  "tcp_fast_open_queue_length": null,
  "traffic_direction": 0,
  "udp_listener_config": null,
  "api_listener": null,
  "connection_balance_config": null,
  "reuse_port": false,
  "access_log": [
  ],
  "tcp_backlog_size": null,
  "bind_to_port": null
},"http-list-2":{
  "name": "mar",
  "address": null,
  "stat_prefix": "",
  "filter_chains": [
  ],
  "use_original_dst": null,
  "default_filter_chain": null,
  "per_connection_buffer_limit_bytes": null,
  "metadata": null,
  "deprecated_v1": null,
  "drain_type": 0,
  "listener_filters": [
  ],
  "listener_filters_timeout": null,
  "continue_on_listener_filters_timeout": false,
  "transparent": null,
  "freebind": null,
  "socket_options": [
  ],
  "tcp_fast_open_queue_length": null,
  "traffic_direction": 0,
  "udp_listener_config": null,
  "api_listener": null,
  "connection_balance_config": null,
  "reuse_port": false,
  "access_log": [
  ],
  "tcp_backlog_size": null,
  "bind_to_port": null
}}`,
				"_rds": `{"route-1":{
  "name": "mar",
  "virtual_hosts": [
  ],
  "vhds": null,
  "internal_only_headers": [
  ],
  "response_headers_to_add": [
  ],
  "response_headers_to_remove": [
  ],
  "request_headers_to_add": [
  ],
  "request_headers_to_remove": [
  ],
  "most_specific_header_mutations_wins": false,
  "validate_clusters": null,
  "max_direct_response_body_size_bytes": null
}}`,
				"_eds": `{"load-assignment-1":{
  "cluster_name": "foo",
  "endpoints": [
  ],
  "named_endpoints": {
  },
  "policy": null
}}`,
				"_ecds": `{"eds-cluster-1":{
  "transport_socket_matches": [
  ],
  "name": "test",
  "alt_stat_name": "",
  "eds_cluster_config": null,
  "connect_timeout": null,
  "per_connection_buffer_limit_bytes": null,
  "lb_policy": 0,
  "load_assignment": null,
  "health_checks": [
  ],
  "max_requests_per_connection": null,
  "circuit_breakers": null,
  "upstream_http_protocol_options": null,
  "common_http_protocol_options": null,
  "http_protocol_options": null,
  "http2_protocol_options": null,
  "typed_extension_protocol_options": {
  },
  "dns_refresh_rate": null,
  "dns_failure_refresh_rate": null,
  "respect_dns_ttl": false,
  "dns_lookup_family": 0,
  "dns_resolvers": [
  ],
  "use_tcp_for_dns_lookups": false,
  "outlier_detection": null,
  "cleanup_interval": null,
  "upstream_bind_config": null,
  "lb_subset_config": null,
  "common_lb_config": null,
  "transport_socket": null,
  "metadata": null,
  "protocol_selection": 0,
  "upstream_connection_options": null,
  "close_connections_on_host_health_failure": false,
  "ignore_health_on_host_removal": false,
  "filters": [
  ],
  "load_balancing_policy": null,
  "lrs_server": null,
  "track_timeout_budgets": false,
  "upstream_config": null,
  "track_cluster_stats": null,
  "preconnect_policy": null,
  "connection_pool_per_downstream_connection": false,
  "hidden_envoy_deprecated_hosts": [
  ],
  "hidden_envoy_deprecated_tls_context": null,
  "hidden_envoy_deprecated_extension_protocol_options": {
  }
}}`,
				"_cds": `{"cluster-1":{
  "transport_socket_matches": [
  ],
  "name": "foo",
  "alt_stat_name": "",
  "eds_cluster_config": null,
  "connect_timeout": null,
  "per_connection_buffer_limit_bytes": null,
  "lb_policy": 0,
  "load_assignment": null,
  "health_checks": [
  ],
  "max_requests_per_connection": null,
  "circuit_breakers": null,
  "upstream_http_protocol_options": null,
  "common_http_protocol_options": null,
  "http_protocol_options": null,
  "http2_protocol_options": null,
  "typed_extension_protocol_options": {
  },
  "dns_refresh_rate": null,
  "dns_failure_refresh_rate": null,
  "respect_dns_ttl": false,
  "dns_lookup_family": 0,
  "dns_resolvers": [
  ],
  "use_tcp_for_dns_lookups": false,
  "outlier_detection": null,
  "cleanup_interval": null,
  "upstream_bind_config": null,
  "lb_subset_config": null,
  "common_lb_config": null,
  "transport_socket": null,
  "metadata": null,
  "protocol_selection": 0,
  "upstream_connection_options": null,
  "close_connections_on_host_health_failure": false,
  "ignore_health_on_host_removal": false,
  "filters": [
  ],
  "load_balancing_policy": null,
  "lrs_server": null,
  "track_timeout_budgets": false,
  "upstream_config": null,
  "track_cluster_stats": null,
  "preconnect_policy": null,
  "connection_pool_per_downstream_connection": false,
  "hidden_envoy_deprecated_hosts": [
  ],
  "hidden_envoy_deprecated_tls_context": null,
  "hidden_envoy_deprecated_extension_protocol_options": {
  }
}}`,
			},
			adsc: &ADSC{
				tcpListeners: map[string]*listener.Listener{
					"listener-1": {
						Name: "bar",
					},
					"listener-2": {
						Name: "mar",
					},
				},
				httpListeners: map[string]*listener.Listener{
					"http-list-1": {
						Name: "bar",
					},
					"http-list-2": {
						Name: "mar",
					},
				},
				routes: map[string]*route.RouteConfiguration{
					"route-1": {
						Name: "mar",
					},
				},
				edsClusters: map[string]*cluster.Cluster{
					"eds-cluster-1": {
						Name: "test",
					},
				},
				clusters: map[string]*cluster.Cluster{
					"cluster-1": {
						Name: "foo",
					},
				},
				eds: map[string]*endpoint.ClusterLoadAssignment{
					"load-assignment-1": {
						ClusterName: "foo",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			base := t.TempDir()
			if err := tt.adsc.Save(base); (err == nil && tt.err != nil) || (err != nil && tt.err == nil) {
				t.Errorf("AdscSave() => %v expected err %v", err, tt.err)
			}
			if ldsTCP := readFile(base+"_lds_tcp.json", t); ldsTCP != tt.expectedJSON["_lds_tcp"] {
				t.Errorf("AdscSave() => %s expected ldsTcp %s\n aaa \n%v", ldsTCP, tt.expectedJSON["_lds_tcp"], cmp.Diff(ldsTCP, tt.expectedJSON["_lds_tcp"]))
			}
			if ldsHTTP := readFile(base+"_lds_http.json", t); ldsHTTP != tt.expectedJSON["_lds_http"] {
				t.Errorf("AdscSave() => %s expected ldsHttp %s", ldsHTTP, tt.expectedJSON["_lds_http"])
			}
			if rds := readFile(base+"_rds.json", t); rds != tt.expectedJSON["_rds"] {
				t.Errorf("AdscSave() => %s expected rds %s", rds, tt.expectedJSON["_rds"])
			}
			if ecds := readFile(base+"_ecds.json", t); ecds != tt.expectedJSON["_ecds"] {
				t.Errorf("AdscSave() => %s expected ecds %s", ecds, tt.expectedJSON["_ecds"])
			}
			if cds := readFile(base+"_cds.json", t); cds != tt.expectedJSON["_cds"] {
				t.Errorf("AdscSave() => %s expected cds %s", cds, tt.expectedJSON["_cds"])
			}
			if eds := readFile(base+"_eds.json", t); eds != tt.expectedJSON["_eds"] {
				t.Errorf("AdscSave() => %s expected eds %s", eds, tt.expectedJSON["_eds"])
			}
			saveTeardown(base, t)
		})
	}
}

func saveTeardown(base string, t *testing.T) {
	if err := os.Remove(base + "_lds_tcp.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_lds_http.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_cds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_rds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_ecds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
	if err := os.Remove(base + "_eds.json"); err != nil {
		t.Errorf("Unable to cleanup: %v", err)
	}
}

func readFile(dir string, t *testing.T) string {
	dat, err := ioutil.ReadFile(dir)
	if err != nil {
		t.Fatalf("file %s issue: %v", dat, err)
	}
	return string(dat)
}

func TestADSC_handleMCP(t *testing.T) {
	adsc := &ADSC{
		VersionInfo: map[string]string{},
		Store:       model.MakeIstioStore(memory.Make(collections.Pilot)),
	}

	tests := []struct {
		desc              string
		resources         []*any.Any
		expectedResources [][]string
	}{
		{
			desc: "create-resources",
			resources: []*any.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.1"),
				constructResource("foo2", "foo2.bar.com", "192.1.1.2"),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.1.1.2"},
			},
		},
		{
			desc: "update-and-create-resources",
			resources: []*any.Any{
				constructResource("foo1", "foo1.bar.com", "192.1.1.1"),
				constructResource("foo2", "foo2.bar.com", "192.2.2.2"),
				constructResource("foo3", "foo2.bar.com", "192.1.1.3"),
			},
			expectedResources: [][]string{
				{"foo1", "foo1.bar.com", "192.1.1.1"},
				{"foo2", "foo2.bar.com", "192.2.2.2"},
				{"foo3", "foo2.bar.com", "192.1.1.3"},
			},
		},
		{
			desc: "delete-and-create-resources",
			resources: []*any.Any{
				constructResource("foo4", "foo4.bar.com", "192.1.1.4"),
			},
			expectedResources: [][]string{
				{"foo4", "foo4.bar.com", "192.1.1.4"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gvk := []string{"networking.istio.io", "v1alpha3", "ServiceEntry"}
			adsc.handleMCP(gvk, tt.resources)
			configs, _ := adsc.Store.List(collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(), "")
			if len(configs) != len(tt.expectedResources) {
				t.Errorf("expecte %v got %v", len(tt.expectedResources), len(configs))
			}
			configMap := make(map[string][]string)
			for _, conf := range configs {
				service, _ := conf.Spec.(*networking.ServiceEntry)
				configMap[conf.Name] = []string{conf.Name, service.Hosts[0], service.Addresses[0]}
			}
			for _, expected := range tt.expectedResources {
				got, ok := configMap[expected[0]]
				if !ok {
					t.Errorf("expecte %v got none", expected)
				} else {
					for i, value := range expected {
						if value != got[i] {
							t.Errorf("expecte %v got %v", value, got[i])
						}
					}
				}
			}
		})
	}
}

func constructResource(name string, host string, address string) *any.Any {
	service := &networking.ServiceEntry{
		Hosts:     []string{host},
		Addresses: []string{address},
	}
	seAny, _ := types.MarshalAny(service)
	resource := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:       "default/" + name,
			CreateTime: types.TimestampNow(),
		},
		Body: seAny,
	}
	resAny, _ := types.MarshalAny(resource)
	return &any.Any{
		TypeUrl: resAny.TypeUrl,
		Value:   resAny.Value,
	}
}
