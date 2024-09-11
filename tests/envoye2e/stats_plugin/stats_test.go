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

package client

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	// Preload proto definitions
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/envoye2e"
	"istio.io/istio/tests/envoye2e/driver"
	"istio.io/istio/tests/envoye2e/env"
)

type capture struct{}

func (capture) Run(p *driver.Params) error {
	prev, err := strconv.Atoi(p.Vars["RequestCount"])
	if err != nil {
		return err
	}
	p.Vars["RequestCount"] = fmt.Sprintf("%d", p.N+prev)
	return nil
}
func (capture) Cleanup() {}

var Runtimes = []struct {
	WasmRuntime string
}{
	{
		// native filter
	},
}

var TestCases = []struct {
	Name                string
	ClientConfig        string
	ServerConfig        string
	ServerClusterName   string
	ClientStats         map[string]driver.StatMatcher
	ServerStats         map[string]driver.StatMatcher
	TestParallel        bool
	ElideServerMetadata bool
}{
	{
		Name:         "Default",
		ClientConfig: "testdata/stats/client_config.yaml",
		ServerConfig: "testdata/stats/server_config.yaml",
		ClientStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_request_total.yaml.tmpl"},
		},
		ServerStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
			"istio_build":          &driver.ExactStat{Metric: "testdata/metric/istio_build.yaml"},
		},
		TestParallel: true,
	},
	{
		Name:         "Customized",
		ClientConfig: "testdata/stats/client_config_customized.yaml.tmpl",
		ServerConfig: "testdata/stats/server_config.yaml",
		ClientStats: map[string]driver.StatMatcher{
			"istio_custom":                        &driver.ExactStat{Metric: "testdata/metric/client_custom_metric.yaml.tmpl"},
			"istio_requests_total":                &driver.ExactStat{Metric: "testdata/metric/client_request_total_customized.yaml.tmpl"},
			"istio_request_duration_milliseconds": &driver.MissingStat{Metric: "istio_request_duration_milliseconds"},
		},
		ServerStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
			"istio_build":          &driver.ExactStat{Metric: "testdata/metric/istio_build.yaml"},
		},
		TestParallel: true,
	},
	{
		Name:              "UseHostHeader",
		ClientConfig:      "testdata/stats/client_config.yaml",
		ServerConfig:      "testdata/stats/server_config.yaml",
		ServerClusterName: "host_header",
		ClientStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/host_header_fallback.yaml.tmpl"},
		},
		ServerStats: map[string]driver.StatMatcher{},
	},
	{
		Name:              "DisableHostHeader",
		ClientConfig:      "testdata/stats/client_config_disable_header_fallback.yaml",
		ServerConfig:      "testdata/stats/server_config_disable_header_fallback.yaml",
		ServerClusterName: "host_header",
		ClientStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_disable_host_header_fallback.yaml.tmpl"},
		},
		ServerStats: map[string]driver.StatMatcher{
			"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_disable_host_header_fallback.yaml.tmpl"},
		},
		ElideServerMetadata: true,
	},
}

func enableStats(t *testing.T, vars map[string]string) {
	t.Helper()
	vars["ServerHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_native_inbound.yaml.tmpl") + "\n" +
		driver.LoadTestData("testdata/filters/stats_inbound.yaml.tmpl")
	vars["ClientHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_native_outbound.yaml.tmpl") + "\n" +
		driver.LoadTestData("testdata/filters/stats_outbound.yaml.tmpl")
}

func TestStatsPayload(t *testing.T) {
	for _, testCase := range TestCases {
		for _, runtime := range Runtimes {
			t.Run(testCase.Name+"/"+runtime.WasmRuntime, func(t *testing.T) {
				clientStats := make(map[string]driver.StatMatcher)
				for metric, values := range testCase.ClientStats {
					clientStats[metric] = values
				}
				params := driver.NewTestParams(t, map[string]string{
					"RequestCount":            "10",
					"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
					"StatsFilterClientConfig": driver.LoadTestJSON(testCase.ClientConfig),
					"StatsFilterServerConfig": driver.LoadTestJSON(testCase.ServerConfig),
					"ServerClusterName":       testCase.ServerClusterName,
					"ElideServerMetadata":     fmt.Sprintf("%t", testCase.ElideServerMetadata),
				}, envoye2e.ProxyE2ETests)
				params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
				params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
				enableStats(t, params.Vars)
				if err := (&driver.Scenario{
					Steps: []driver.Step{
						&driver.XDS{},
						&driver.Update{
							Node:      "client",
							Version:   "0",
							Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
							Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")},
						},
						&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
						&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
						&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
						&driver.Sleep{Duration: 1 * time.Second},
						&driver.Repeat{
							N: 10,
							Step: &driver.HTTPCall{
								Port: params.Ports.ClientPort,
								Body: "hello, world!",
							},
						},
						&driver.Stats{AdminPort: params.Ports.ClientAdmin, Matchers: clientStats},
						&driver.Stats{AdminPort: params.Ports.ServerAdmin, Matchers: testCase.ServerStats},
					},
				}).Run(params); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestStatsParallel(t *testing.T) {
	env.SkipTSanASan(t)
	for _, testCase := range TestCases {
		t.Run(testCase.Name, func(t *testing.T) {
			if !testCase.TestParallel {
				t.Skip("Skip parallel testing")
			}
			params := driver.NewTestParams(t, map[string]string{
				"RequestCount":            "1",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON(testCase.ClientConfig),
				"StatsFilterServerConfig": driver.LoadTestJSON(testCase.ServerConfig),
				"ElideServerMetadata":     fmt.Sprintf("%t", testCase.ElideServerMetadata),
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			clientRequestTotal := &dto.MetricFamily{}
			serverRequestTotal := &dto.MetricFamily{}
			params.LoadTestProto("testdata/metric/client_request_total.yaml.tmpl", clientRequestTotal)
			params.LoadTestProto("testdata/metric/server_request_total.yaml.tmpl", serverRequestTotal)
			enableStats(t, params.Vars)

			clientListenerTemplate := driver.LoadTestData("testdata/listener/client.yaml.tmpl")
			serverListenerTemplate := driver.LoadTestData("testdata/listener/server.yaml.tmpl")

			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{Node: "client", Version: "0", Listeners: []string{clientListenerTemplate}},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{serverListenerTemplate}},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					driver.Get(params.Ports.ClientPort, "hello, world!"),
					&driver.Fork{
						Fore: &driver.Scenario{
							Steps: []driver.Step{
								&driver.Sleep{Duration: 1 * time.Second},
								&driver.Repeat{
									Duration: 9 * time.Second,
									Step:     driver.Get(params.Ports.ClientPort, "hello, world!"),
								},
								capture{},
							},
						},
						Back: &driver.Repeat{
							Duration: 10 * time.Second,
							Step: &driver.Scenario{
								Steps: []driver.Step{
									&driver.Update{
										Node:      "client",
										Version:   "{{.N}}",
										Listeners: []string{clientListenerTemplate},
									},
									&driver.Update{
										Node:      "server",
										Version:   "{{.N}}",
										Listeners: []string{serverListenerTemplate},
									},
									// may need short delay so we don't eat all the CPU
									&driver.Sleep{Duration: 100 * time.Millisecond},
								},
							},
						},
					},
					&driver.Stats{AdminPort: params.Ports.ClientAdmin, Matchers: testCase.ClientStats},
					&driver.Stats{AdminPort: params.Ports.ServerAdmin, Matchers: testCase.ServerStats},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStatsGrpc(t *testing.T) {
	env.SkipTSan(t)
	for _, runtime := range Runtimes {
		t.Run(runtime.WasmRuntime, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{
				"RequestCount":            "10",
				"DisableDirectResponse":   "true",
				"UsingGrpcBackend":        "true",
				"GrpcResponseStatus":      "7",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config.yaml"),
				"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config.yaml"),
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			enableStats(t, params.Vars)

			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					&driver.GrpcServer{},
					&driver.GrpcCall{
						ReqCount:   10,
						WantStatus: status.New(codes.PermissionDenied, "denied"),
					},
					&driver.Stats{
						AdminPort: params.Ports.ServerAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
						},
					},
					&driver.Stats{
						AdminPort: params.Ports.ClientAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_request_total.yaml.tmpl"},
						},
					},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStatsGrpcStream(t *testing.T) {
	env.SkipTSan(t)
	for _, runtime := range Runtimes {
		t.Run(runtime.WasmRuntime, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{
				"DisableDirectResponse":   "true",
				"UsingGrpcBackend":        "true",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config_grpc.yaml.tmpl"),
				"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config_grpc.yaml.tmpl"),
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			enableStats(t, params.Vars)
			params.Vars["ClientHTTPFilters"] = params.LoadTestData("testdata/filters/grpc_stats.yaml") + params.Vars["ClientHTTPFilters"]
			params.Vars["ServerHTTPFilters"] = params.LoadTestData("testdata/filters/grpc_stats.yaml") + params.Vars["ServerHTTPFilters"]

			bidi := &driver.GrpcStream{}
			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					&driver.GrpcServer{},
					bidi,
					// Send a first batch of messages on the stream and check stats
					bidi.Send([]uint32{1, 5, 7}),
					driver.StepFunction(func(p *driver.Params) error {
						p.Vars["RequestMessages"] = "3"
						p.Vars["ResponseMessages"] = "13"
						return nil
					}),
					&driver.Stats{
						AdminPort: params.Ports.ServerAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_request_messages_total":  &driver.ExactStat{Metric: "testdata/metric/server_request_messages.yaml.tmpl"},
							"istio_response_messages_total": &driver.ExactStat{Metric: "testdata/metric/server_response_messages.yaml.tmpl"},
						},
					},
					&driver.Stats{
						AdminPort: params.Ports.ClientAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_request_messages_total":  &driver.ExactStat{Metric: "testdata/metric/client_request_messages.yaml.tmpl"},
							"istio_response_messages_total": &driver.ExactStat{Metric: "testdata/metric/client_response_messages.yaml.tmpl"},
						},
					},
					// Send and close
					bidi.Send([]uint32{10, 1, 1, 1, 1}),
					bidi.Close(),
					driver.StepFunction(func(p *driver.Params) error {
						p.Vars["RequestMessages"] = "8"
						p.Vars["ResponseMessages"] = "27"
						return nil
					}),
					&driver.Stats{
						AdminPort: params.Ports.ServerAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_request_messages_total":  &driver.ExactStat{Metric: "testdata/metric/server_request_messages.yaml.tmpl"},
							"istio_response_messages_total": &driver.ExactStat{Metric: "testdata/metric/server_response_messages.yaml.tmpl"},
						},
					},
					&driver.Stats{
						AdminPort: params.Ports.ClientAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_request_messages_total":  &driver.ExactStat{Metric: "testdata/metric/client_request_messages.yaml.tmpl"},
							"istio_response_messages_total": &driver.ExactStat{Metric: "testdata/metric/client_response_messages.yaml.tmpl"},
						},
					},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAttributeGen(t *testing.T) {
	env.EnsureWasmFiles(t)
	env.SkipTSan(t)
	params := driver.NewTestParams(t, map[string]string{
		"RequestCount":             "10",
		"AttributeGenFilterConfig": "filename: " + testenv.LocalOut + "/attributegen.wasm",
		"StatsFilterClientConfig":  driver.LoadTestJSON("testdata/stats/client_config.yaml"),
		"StatsFilterServerConfig":  driver.LoadTestJSON("testdata/stats/request_classification_config.yaml"),
		"ResponseCodeClass":        "2xx",
	}, envoye2e.ProxyE2ETests)
	params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
	params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
	enableStats(t, params.Vars)
	params.Vars["ServerHTTPFilters"] = driver.LoadTestData("testdata/filters/attributegen.yaml.tmpl") + "\n" +
		params.Vars["ServerHTTPFilters"]
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Repeat{
				N: 10,
				Step: &driver.HTTPCall{
					Port: params.Ports.ClientPort,
					Body: "hello, world!",
				},
			},
			&driver.Stats{AdminPort: params.Ports.ServerAdmin, Matchers: map[string]driver.StatMatcher{
				"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
			}},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestStatsParserRegression(t *testing.T) {
	env.SkipTSan(t)
	// This is a regression test for https://github.com/envoyproxy/envoy-wasm/issues/497
	params := driver.NewTestParams(t, map[string]string{
		"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
		"ClientHTTPFilters":       driver.LoadTestData("testdata/filters/stats_outbound.yaml.tmpl"),
		"StatsFilterClientConfig": "{}",
	}, envoye2e.ProxyE2ETests)
	listener0 := params.LoadTestData("testdata/listener/client.yaml.tmpl")
	params.Vars["StatsFilterClientConfig"] = driver.LoadTestJSON("testdata/stats/client_config_customized.yaml.tmpl")
	listener1 := params.LoadTestData("testdata/listener/client.yaml.tmpl")
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node:      "client",
				Version:   "0",
				Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
				Listeners: []string{listener0},
			},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Update{
				Node:      "client",
				Version:   "1",
				Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
				Listeners: []string{listener1},
			},
			&driver.Sleep{Duration: 1 * time.Second},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestStats403Failure(t *testing.T) {
	env.SkipTSan(t)
	for _, runtime := range Runtimes {
		t.Run(runtime.WasmRuntime, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{
				"RequestCount":            "10",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config.yaml"),
				"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config.yaml"),
				"ResponseCode":            "403",
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			enableStats(t, params.Vars)
			params.Vars["ServerHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_native_inbound.yaml.tmpl") + "\n" +
				params.LoadTestData("testdata/filters/rbac.yaml.tmpl") + "\n" +
				driver.LoadTestData("testdata/filters/stats_inbound.yaml.tmpl")
			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					&driver.Repeat{
						N: 10,
						Step: &driver.HTTPCall{
							Port:         params.Ports.ClientPort,
							Body:         "RBAC: access denied",
							ResponseCode: 403,
						},
					},
					&driver.Stats{AdminPort: params.Ports.ServerAdmin, Matchers: map[string]driver.StatMatcher{
						"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
					}},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStatsECDS(t *testing.T) {
	env.SkipTSan(t)
	for _, runtime := range Runtimes {
		t.Run(runtime.WasmRuntime, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{
				"RequestCount":            "10",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config.yaml"),
				"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config.yaml"),
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			params.Vars["ServerHTTPFilters"] = params.LoadTestData("testdata/filters/extension_config_inbound.yaml.tmpl")
			params.Vars["ClientHTTPFilters"] = params.LoadTestData("testdata/filters/extension_config_outbound.yaml.tmpl")

			updateExtensions := &driver.UpdateExtensions{
				Extensions: []string{
					driver.LoadTestData("testdata/filters/mx_native_inbound.yaml.tmpl"),
					driver.LoadTestData("testdata/filters/stats_inbound.yaml.tmpl"),
					driver.LoadTestData("testdata/filters/mx_native_outbound.yaml.tmpl"),
					driver.LoadTestData("testdata/filters/stats_outbound.yaml.tmpl"),
				},
			}
			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{
						Node:      "client",
						Version:   "0",
						Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
						Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")},
					},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
					updateExtensions,
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					&driver.Repeat{
						N: 10,
						Step: &driver.HTTPCall{
							Port: params.Ports.ClientPort,
							Body: "hello, world!",
						},
					},
					&driver.Stats{AdminPort: params.Ports.ClientAdmin, Matchers: map[string]driver.StatMatcher{
						"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_request_total.yaml.tmpl"},
					}},
					&driver.Stats{AdminPort: params.Ports.ServerAdmin, Matchers: map[string]driver.StatMatcher{
						"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_request_total.yaml.tmpl"},
					}},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStatsEndpointLabels(t *testing.T) {
	env.SkipTSan(t)
	for _, runtime := range Runtimes {
		t.Run(runtime.WasmRuntime, func(t *testing.T) {
			params := driver.NewTestParams(t, map[string]string{
				"RequestCount":            "10",
				"EnableEndpointMetadata":  "true",
				"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
				"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config.yaml"),
			}, envoye2e.ProxyE2ETests)
			params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
			params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
			params.Vars["ClientHTTPFilters"] = driver.LoadTestData("testdata/filters/stats_outbound.yaml.tmpl")
			if err := (&driver.Scenario{
				Steps: []driver.Step{
					&driver.XDS{},
					&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
					&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
					&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
					&driver.Sleep{Duration: 1 * time.Second},
					&driver.Repeat{
						N: 10,
						Step: &driver.HTTPCall{
							Port:         params.Ports.ClientPort,
							ResponseCode: 200,
						},
					},
					&driver.Stats{
						AdminPort: params.Ports.ClientAdmin,
						Matchers: map[string]driver.StatMatcher{
							"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_request_total_endpoint_labels.yaml.tmpl"},
						},
					},
				},
			}).Run(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const BackendMetadata = `
namespace: default
workload_name: ratings-v1
canonical_name: ratings
canonical_revision: version-1
uid: //v1/pod/default/ratings
service_account: ratings
trust_domain: cluster.global
`

const ProductPageMetadata = `
workload_name: productpage-v1
uid: //v1/pod/default/productpage
`

func TestStatsServerWaypointProxy(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{
		"RequestCount":            "10",
		"EnableDelta":             "true",
		"EnableMetadataDiscovery": "true",
		"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_waypoint_proxy_config.yaml"),
	}, envoye2e.ProxyE2ETests)
	params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
	params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_waypoint_proxy_node_metadata.json.tmpl")
	params.Vars["ServerHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_native_inbound.yaml.tmpl") + "\n" +
		driver.LoadTestData("testdata/filters/mx_waypoint.yaml.tmpl") + "\n" +
		driver.LoadTestData("testdata/filters/stats_inbound.yaml.tmpl")
	params.Vars["ClientHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_native_outbound.yaml.tmpl")

	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{Node: "client", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")}},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.UpdateWorkloadMetadata{Workloads: []driver.WorkloadMetadata{{
				Address:  "127.0.0.3",
				Metadata: BackendMetadata,
			}}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Repeat{
				N: 10,
				Step: &driver.HTTPCall{
					Port:         params.Ports.ClientPort,
					ResponseCode: 200,
				},
			},
			&driver.Stats{
				AdminPort: params.Ports.ServerAdmin,
				Matchers: map[string]driver.StatMatcher{
					"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_waypoint_proxy_request_total.yaml.tmpl"},
				},
			},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestStatsServerWaypointProxyCONNECT(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{
		"RequestCount":            "10",
		"EnableDelta":             "true",
		"EnableMetadataDiscovery": "true",
		"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_waypoint_proxy_config.yaml"),
	}, envoye2e.ProxyE2ETests)
	params.Vars["ServerClusterName"] = "internal_outbound"
	params.Vars["ServerInternalAddress"] = "internal_inbound"
	params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_waypoint_proxy_node_metadata.json.tmpl")
	params.Vars["ServerHTTPFilters"] = driver.LoadTestData("testdata/filters/mx_waypoint.yaml.tmpl") + "\n" +
		driver.LoadTestData("testdata/filters/stats_inbound.yaml.tmpl")
	params.Vars["EnableTunnelEndpointMetadata"] = "true"
	params.Vars["EnableOriginalDstPortOverride"] = "true"

	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node: "client", Version: "0",
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
			},
			&driver.Update{
				Node: "server", Version: "0",
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
			},
			&driver.UpdateWorkloadMetadata{Workloads: []driver.WorkloadMetadata{{
				Address:  "127.0.0.1",
				Metadata: ProductPageMetadata,
			}, {
				Address:  "127.0.0.3",
				Metadata: BackendMetadata,
			}}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Repeat{
				N: 10,
				Step: &driver.HTTPCall{
					Port:         params.Ports.ClientPort,
					ResponseCode: 200,
				},
			},
			&driver.Stats{
				AdminPort: params.Ports.ServerAdmin,
				Matchers: map[string]driver.StatMatcher{
					"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/server_waypoint_proxy_connect_request_total.yaml.tmpl"},
				},
			},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestStatsExpiry(t *testing.T) {
	params := driver.NewTestParams(t, map[string]string{
		"RequestCount":            "1",
		"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
		"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config_expiry.yaml"),
		"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config.yaml"),
	}, envoye2e.ProxyE2ETests)
	params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
	params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
	enableStats(t, params.Vars)
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node:      "client",
				Version:   "0",
				Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
				Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")},
			},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Repeat{
				N: 1,
				Step: &driver.HTTPCall{
					Port: params.Ports.ClientPort,
					Body: "hello, world!",
				},
			},
			&driver.Sleep{Duration: 4 * time.Second},
			&driver.Stats{AdminPort: params.Ports.ClientAdmin, Matchers: map[string]driver.StatMatcher{
				"istio_requests_total": &driver.MissingStat{Metric: "istio_requests_total"},
				"istio_build":          &driver.ExactStat{Metric: "testdata/metric/istio_build.yaml"},
			}},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}

func TestStatsDestinationServiceNamespacePrecedence(t *testing.T) {
	clientStats := map[string]driver.StatMatcher{
		"istio_requests_total": &driver.ExactStat{Metric: "testdata/metric/client_request_total_cluster_metadata_precedence.yaml.tmpl"},
	}
	params := driver.NewTestParams(t, map[string]string{
		"RequestCount":            "10",
		"StatsConfig":             driver.LoadTestData("testdata/bootstrap/stats.yaml.tmpl"),
		"StatsFilterClientConfig": driver.LoadTestJSON("testdata/stats/client_config.yaml"),
		"StatsFilterServerConfig": driver.LoadTestJSON("testdata/stats/server_config.yaml"),
	}, envoye2e.ProxyE2ETests)
	params.Vars["ClientMetadata"] = params.LoadTestData("testdata/client_node_metadata.json.tmpl")
	params.Vars["ServerMetadata"] = params.LoadTestData("testdata/server_node_metadata.json.tmpl")
	enableStats(t, params.Vars)
	if err := (&driver.Scenario{
		Steps: []driver.Step{
			&driver.XDS{},
			&driver.Update{
				Node:      "client",
				Version:   "0",
				Clusters:  []string{params.LoadTestData("testdata/cluster/server.yaml.tmpl")},
				Listeners: []string{params.LoadTestData("testdata/listener/client.yaml.tmpl")},
			},
			&driver.Update{Node: "server", Version: "0", Listeners: []string{params.LoadTestData("testdata/listener/server.yaml.tmpl")}},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/server.yaml.tmpl")},
			&driver.Envoy{Bootstrap: params.LoadTestData("testdata/bootstrap/client_cluster_metadata_precedence.yaml.tmpl")},
			&driver.Sleep{Duration: 1 * time.Second},
			&driver.Repeat{
				N: 10,
				Step: &driver.HTTPCall{
					Port: params.Ports.ClientPort,
					Body: "hello, world!",
				},
			},
			&driver.Stats{AdminPort: params.Ports.ClientAdmin, Matchers: clientStats},
		},
	}).Run(params); err != nil {
		t.Fatal(err)
	}
}
