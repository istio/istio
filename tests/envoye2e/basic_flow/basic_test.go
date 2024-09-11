// Copyright 2019 Istio Authors
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
	"strconv"
	"testing"
	"time"

	"istio.io/istio/tests/envoye2e"
	"istio.io/istio/tests/envoye2e/driver"
)

var ProtocolOptions = []struct {
	Name string
	Quic bool
}{
	{
		Name: "h2",
		Quic: false,
	},
	{
		Name: "quic",
		Quic: true,
	},
}

func TestBasicTCPFlow(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{
		"ConnectionCount":       "10",
		"DisableDirectResponse": "true",
	}, envoye2e.ProxyE2ETests)
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node:      "client",
				Version:   "0",
				Clusters:  []string{driver.LoadTestData("testdata/cluster/tcp_client.yaml.tmpl")},
				Listeners: []string{driver.LoadTestData("testdata/listener/tcp_client.yaml.tmpl")},
			},
			&driver.Update{
				Node:      "server",
				Version:   "0",
				Clusters:  []string{driver.LoadTestData("testdata/cluster/tcp_server.yaml.tmpl")},
				Listeners: []string{driver.LoadTestData("testdata/listener/tcp_server.yaml.tmpl")},
			},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.TCPServer{Prefix: "hello"},
			&driver.Repeat{
				N:    10,
				Step: &driver.TCPConnection{},
			},
			&driver.Stats{
				AdminPort: params.Ports.ServerAdmin,
				Matchers: map[string]driver.StatMatcher{
					"envoy_tcp_downstream_cx_total": &driver.PartialStat{Metric: "testdata/metric/basic_flow_server_tcp_connection.yaml.tmpl"},
				},
			},
			&driver.Stats{
				AdminPort: params.Ports.ClientAdmin,
				Matchers: map[string]driver.StatMatcher{
					"envoy_tcp_downstream_cx_total": &driver.PartialStat{Metric: "testdata/metric/basic_flow_client_tcp_connection.yaml.tmpl"},
				},
			},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestBasicHTTP(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{}, envoye2e.ProxyE2ETests)
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{Node: "client", Version: "0", Listeners: []string{driver.LoadTestData("testdata/listener/client.yaml.tmpl")}},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{driver.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			driver.Get(params.Ports.ClientPort, "hello, world!"),
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestBasicHTTPwithTLS(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{}, envoye2e.ProxyE2ETests)
	params.Vars["ClientTLSContext"] = params.LoadTestData("testdata/transport_socket/client.yaml.tmpl")
	params.Vars["ServerTLSContext"] = params.LoadTestData("testdata/transport_socket/server.yaml.tmpl")
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{Node: "client", Version: "0", Listeners: []string{driver.LoadTestData("testdata/listener/client.yaml.tmpl")}},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{driver.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			driver.Get(params.Ports.ClientPort, "hello, world!"),
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

// Tests with a single combined proxy hosting inbound/outbound listeners
func TestBasicHTTPGateway(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{}, envoye2e.ProxyE2ETests)
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node: "server", Version: "0",
				Clusters: []string{driver.LoadTestData("testdata/cluster/server.yaml.tmpl")},
				Listeners: []string{
					driver.LoadTestData("testdata/listener/client.yaml.tmpl"),
					driver.LoadTestData("testdata/listener/server.yaml.tmpl"),
				},
			},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			driver.Get(params.Ports.ClientPort, "hello, world!"),
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

var ConnectServer = &driver.Update{
	Node: "server", Version: "{{ .N }}",
	Clusters: []string{
		driver.LoadTestData("testdata/cluster/internal_inbound.yaml.tmpl"),
	},
	Listeners: []string{
		driver.LoadTestData("testdata/listener/terminate_connect.yaml.tmpl"),
		driver.LoadTestData("testdata/listener/server.yaml.tmpl"),
	},
	Secrets: []string{
		driver.LoadTestData("testdata/secret/server.yaml.tmpl"),
	},
}

func TestBasicCONNECT(t *testing.T) {
	for _, options := range ProtocolOptions {
		t.Run(options.Name, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{}, envoye2e.ProxyE2ETests)
			params.Vars["ServerClusterName"] = "internal_outbound"
			params.Vars["ServerInternalAddress"] = "internal_inbound"
			params.Vars["quic"] = strconv.FormatBool(options.Quic)
			params.Vars["EnableTunnelEndpointMetadata"] = "true"
			params.Vars["EnableOriginalDstPortOverride"] = "true"

			updateClient := &driver.Update{
				Node: "client", Version: "{{ .N }}",
				Clusters: []string{
					driver.LoadTestData("testdata/cluster/internal_outbound.yaml.tmpl"),
					driver.LoadTestData("testdata/cluster/original_dst.yaml.tmpl"),
				},
				Listeners: []string{
					driver.LoadTestData("testdata/listener/client.yaml.tmpl"),
					driver.LoadTestData("testdata/listener/internal_outbound.yaml.tmpl"),
				},
				Secrets: []string{
					driver.LoadTestData("testdata/secret/client.yaml.tmpl"),
				},
			}

			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					updateClient, ConnectServer,
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					driver.Get(params.Ports.ClientPort, "hello, world!"),
					// xDS load generator:
					// &driver.Repeat{
					// 	Duration: time.Second * 20,
					// 	Step: &driver.Scenario{
					// 		[]driver.Step{
					// 			&driver.Sleep{10000 * time.Millisecond},
					// 			updateClient, updateServer,
					// 			// may need short delay so we don't eat all the CPU
					// 		},
					// 	},
					// },
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPassthroughCONNECT(t *testing.T) {
	for _, options := range ProtocolOptions {
		t.Run(options.Name, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{}, envoye2e.ProxyE2ETests)
			params.Vars["ServerClusterName"] = "internal_outbound"
			params.Vars["ServerInternalAddress"] = "internal_inbound"
			params.Vars["quic"] = strconv.FormatBool(options.Quic)
			params.Vars["EnableOriginalDstPortOverride"] = "true"

			updateClient := &driver.Update{
				Node: "client", Version: "{{ .N }}",
				Clusters: []string{
					driver.LoadTestData("testdata/cluster/tcp_passthrough.yaml.tmpl"),
					driver.LoadTestData("testdata/cluster/internal_outbound.yaml.tmpl"),
					driver.LoadTestData("testdata/cluster/original_dst.yaml.tmpl"),
				},
				Listeners: []string{
					driver.LoadTestData("testdata/listener/client_passthrough.yaml.tmpl"),
					driver.LoadTestData("testdata/listener/tcp_passthrough.yaml.tmpl"),
					driver.LoadTestData("testdata/listener/internal_outbound.yaml.tmpl"),
				},
				Secrets: []string{
					driver.LoadTestData("testdata/secret/client.yaml.tmpl"),
				},
			}

			req := driver.Get(params.Ports.ClientPort, "hello, world!")
			req.Authority = fmt.Sprintf("127.0.0.2:%d", params.Ports.ServerPort)

			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					updateClient, ConnectServer,
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					req,
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}
