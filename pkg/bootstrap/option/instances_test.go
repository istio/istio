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

package option_test

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"

	meshAPI "istio.io/api/mesh/v1alpha1"
	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/bootstrap/option"
)

// nolint: lll
func TestOptions(t *testing.T) {
	cases := []struct {
		testName    string
		key         string
		option      option.Instance
		expectError bool
		expected    interface{}
	}{
		{
			testName: "proxy config",
			key:      "config",
			option:   option.ProxyConfig(&model.NodeMetaProxyConfig{DiscoveryAddress: "fake"}),
			expected: &model.NodeMetaProxyConfig{DiscoveryAddress: "fake"},
		},
		{
			testName: "pilotSAN",
			key:      "pilot_SAN",
			option:   option.PilotSubjectAltName([]string{"fake"}),
			expected: `[{"exact":"fake"}]`,
		},
		{
			testName: "pilotSAN multi",
			key:      "pilot_SAN",
			option:   option.PilotSubjectAltName([]string{"fake", "other"}),
			expected: `[{"exact":"fake"},{"exact":"other"}]`,
		},
		{
			testName: "nil connect timeout",
			key:      "connect_timeout",
			option:   option.ConnectTimeout(nil),
			expected: nil,
		},
		{
			testName: "connect timeout",
			key:      "connect_timeout",
			option:   option.ConnectTimeout(durationpb.New(time.Second)),
			expected: "1s",
		},
		{
			testName: "cluster",
			key:      "cluster",
			option:   option.Cluster("fake"),
			expected: "fake",
		},
		{
			testName: "nodeID",
			key:      "nodeID",
			option:   option.NodeID("fake"),
			expected: "fake",
		},
		{
			testName: "region",
			key:      "region",
			option:   option.Region("fake"),
			expected: "fake",
		},
		{
			testName: "zone",
			key:      "zone",
			option:   option.Zone("fake"),
			expected: "fake",
		},
		{
			testName: "sub zone",
			key:      "sub_zone",
			option:   option.SubZone("fake"),
			expected: "fake",
		},
		{
			testName: "node metadata nil",
			key:      "meta_json_str",
			option:   option.NodeMetadata(nil, nil),
			expected: nil,
		},
		{
			testName: "node metadata",
			key:      "meta_json_str",
			option: option.NodeMetadata(&model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					IstioVersion: "fake",
				},
			}, map[string]interface{}{
				"key": "value",
			}),
			expected: "{\"ISTIO_VERSION\":\"fake\",\"key\":\"value\"}",
		},
		{
			testName: "discovery address",
			key:      "discovery_address",
			option:   option.DiscoveryAddress("fake"),
			expected: "fake",
		},
		{
			testName: "localhost v4",
			key:      "localhost",
			option:   option.Localhost(option.LocalhostIPv4),
			expected: option.LocalhostValue("127.0.0.1"),
		},
		{
			testName: "localhost v6",
			key:      "localhost",
			option:   option.Localhost(option.LocalhostIPv6),
			expected: option.LocalhostValue("::1"),
		},
		{
			testName: "wildcard v4",
			key:      "wildcard",
			option:   option.Wildcard(option.WildcardIPv4),
			expected: option.WildcardValue("0.0.0.0"),
		},
		{
			testName: "wildcard v6",
			key:      "wildcard",
			option:   option.Wildcard(option.WildcardIPv6),
			expected: option.WildcardValue("::"),
		},
		{
			testName: "dns lookup family v4",
			key:      "dns_lookup_family",
			option:   option.DNSLookupFamily(option.DNSLookupFamilyIPv4),
			expected: option.DNSLookupFamilyValue("V4_ONLY"),
		},
		{
			testName: "dns lookup family v6",
			key:      "dns_lookup_family",
			option:   option.DNSLookupFamily(option.DNSLookupFamilyIPv6),
			expected: option.DNSLookupFamilyValue("AUTO"),
		},
		{
			testName: "lightstep address empty",
			key:      "lightstep",
			option:   option.LightstepAddress(""),
			expected: nil,
		},
		{
			testName: "lightstep address ipv4",
			key:      "lightstep",
			option:   option.LightstepAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "lightstep address ipv6",
			key:      "lightstep",
			option:   option.LightstepAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "lightstep address ipv6 missing brackets",
			key:         "lightstep",
			option:      option.LightstepAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "lightstep address host port",
			key:      "lightstep",
			option:   option.LightstepAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "lightstep address no port",
			key:      "lightstep",
			option:   option.LightstepAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "lightstep address missing port",
			key:         "lightstep",
			option:      option.LightstepAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "lightstep token",
			key:      "lightstepToken",
			option:   option.LightstepToken("fake"),
			expected: "fake",
		},
		{
			testName: "openCensusAgent address",
			key:      "openCensusAgent",
			option:   option.OpenCensusAgentAddress("fake-ocagent"),
			expected: "fake-ocagent",
		},
		{
			testName: "openCensusAgent empty context",
			key:      "openCensusAgentContexts",
			option:   option.OpenCensusAgentContexts([]meshAPI.Tracing_OpenCensusAgent_TraceContext{}),
			expected: `["TRACE_CONTEXT","GRPC_TRACE_BIN","CLOUD_TRACE_CONTEXT","B3"]`,
		},
		{
			testName: "openCensusAgent order context",
			key:      "openCensusAgentContexts",
			option: option.OpenCensusAgentContexts([]meshAPI.Tracing_OpenCensusAgent_TraceContext{
				meshAPI.Tracing_OpenCensusAgent_CLOUD_TRACE_CONTEXT,
				meshAPI.Tracing_OpenCensusAgent_B3,
				meshAPI.Tracing_OpenCensusAgent_GRPC_BIN,
				meshAPI.Tracing_OpenCensusAgent_W3C_TRACE_CONTEXT,
			}),
			expected: `["CLOUD_TRACE_CONTEXT","B3","GRPC_TRACE_BIN","TRACE_CONTEXT"]`,
		},
		{
			testName: "openCensusAgent one context",
			key:      "openCensusAgentContexts",
			option: option.OpenCensusAgentContexts([]meshAPI.Tracing_OpenCensusAgent_TraceContext{
				meshAPI.Tracing_OpenCensusAgent_B3,
			}),
			expected: `["B3"]`,
		},
		{
			testName: "stackdriver enabled",
			key:      "stackdriver",
			option:   option.StackDriverEnabled(true),
			expected: true,
		},
		{
			testName: "stackdriver project ID",
			key:      "stackdriverProjectID",
			option:   option.StackDriverProjectID("fake"),
			expected: "fake",
		},
		{
			testName: "stackdriver debug",
			key:      "stackdriverDebug",
			option:   option.StackDriverDebug(true),
			expected: true,
		},
		{
			testName: "stackdriver max annotations",
			key:      "stackdriverMaxAnnotations",
			option:   option.StackDriverMaxAnnotations(100),
			expected: int64(100),
		},
		{
			testName: "stackdriver max attributes",
			key:      "stackdriverMaxAttributes",
			option:   option.StackDriverMaxAttributes(100),
			expected: int64(100),
		},
		{
			testName: "stackdriver max events",
			key:      "stackdriverMaxEvents",
			option:   option.StackDriverMaxEvents(100),
			expected: int64(100),
		},
		{
			testName: "pilot grpc address empty",
			key:      "pilot_grpc_address",
			option:   option.PilotGRPCAddress(""),
			expected: nil,
		},
		{
			testName: "pilot grpc address ipv4",
			key:      "pilot_grpc_address",
			option:   option.PilotGRPCAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "pilot grpc address ipv6",
			key:      "pilot_grpc_address",
			option:   option.PilotGRPCAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "pilot grpc address ipv6 missing brackets",
			key:         "pilot_grpc_address",
			option:      option.PilotGRPCAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "pilot grpc address host port",
			key:      "pilot_grpc_address",
			option:   option.PilotGRPCAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "pilot grpc address no port",
			key:      "pilot_grpc_address",
			option:   option.PilotGRPCAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "pilot grpc address missing port",
			key:         "pilot_grpc_address",
			option:      option.PilotGRPCAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "zipkin address empty",
			key:      "zipkin",
			option:   option.ZipkinAddress(""),
			expected: nil,
		},
		{
			testName: "zipkin address ipv4",
			key:      "zipkin",
			option:   option.ZipkinAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "zipkin address ipv6",
			key:      "zipkin",
			option:   option.ZipkinAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "zipkin address ipv6 missing brackets",
			key:         "zipkin",
			option:      option.ZipkinAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "zipkin address host port",
			key:      "zipkin",
			option:   option.ZipkinAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "zipkin address no port",
			key:      "zipkin",
			option:   option.ZipkinAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "zipkin address missing port",
			key:         "zipkin",
			option:      option.ZipkinAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "datadog address empty",
			key:      "datadog",
			option:   option.DataDogAddress(""),
			expected: nil,
		},
		{
			testName: "datadog address ipv4",
			key:      "datadog",
			option:   option.DataDogAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "datadog address ipv6",
			key:      "datadog",
			option:   option.DataDogAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "datadog address ipv6 missing brackets",
			key:         "datadog",
			option:      option.DataDogAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "datadog address host port",
			key:      "datadog",
			option:   option.DataDogAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "datadog address no port",
			key:      "datadog",
			option:   option.DataDogAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "datadog address missing port",
			key:         "datadog",
			option:      option.DataDogAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "statsd address empty",
			key:      "statsd",
			option:   option.StatsdAddress(""),
			expected: nil,
		},
		{
			testName: "statsd address ipv4",
			key:      "statsd",
			option:   option.StatsdAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "statsd address ipv6",
			key:      "statsd",
			option:   option.StatsdAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "statsd address ipv6 missing brackets",
			key:         "statsd",
			option:      option.StatsdAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "statsd address host port",
			key:      "statsd",
			option:   option.StatsdAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "statsd address no port",
			key:      "statsd",
			option:   option.StatsdAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "statsd address missing port",
			key:         "statsd",
			option:      option.StatsdAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "envoy metrics address empty",
			key:      "envoy_metrics_service_address",
			option:   option.EnvoyMetricsServiceAddress(""),
			expected: nil,
		},
		{
			testName: "envoy metrics address ipv4",
			key:      "envoy_metrics_service_address",
			option:   option.EnvoyMetricsServiceAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "envoy metrics address ipv6",
			key:      "envoy_metrics_service_address",
			option:   option.EnvoyMetricsServiceAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "envoy metrics address ipv6 missing brackets",
			key:         "envoy_metrics_service_address",
			option:      option.EnvoyMetricsServiceAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "envoy metrics address host port",
			key:      "envoy_metrics_service_address",
			option:   option.EnvoyMetricsServiceAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "envoy metrics address no port",
			key:      "envoy_metrics_service_address",
			option:   option.EnvoyMetricsServiceAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "envoy metrics address missing port",
			key:         "envoy_metrics_service_address",
			option:      option.EnvoyMetricsServiceAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "envoy metrics tls nil",
			key:      "envoy_metrics_service_tls",
			option:   option.EnvoyMetricsServiceTLS(nil, &model.BootstrapNodeMetadata{}),
			expected: nil,
		},
		{
			testName: "envoy metrics tls",
			key:      "envoy_metrics_service_tls",
			option: option.EnvoyMetricsServiceTLS(&networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
			}, &model.BootstrapNodeMetadata{}),
			expected: `{"name":"envoy.transport_sockets.tls","typed_config":{"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext","common_tls_context":{"alpn_protocols":["istio","h2"],"combined_validation_context":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"ROOTCA","sds_config":{"api_config_source":{"api_type":"GRPC","grpc_services":[{"envoy_grpc":{"cluster_name":"sds-grpc"}}],"set_node_on_first_message_only":true,"transport_api_version":"V3"},"initial_fetch_timeout":"0s","resource_api_version":"V3"}}},"tls_certificate_sds_secret_configs":[{"name":"default","sds_config":{"api_config_source":{"api_type":"GRPC","grpc_services":[{"envoy_grpc":{"cluster_name":"sds-grpc"}}],"set_node_on_first_message_only":true,"transport_api_version":"V3"},"initial_fetch_timeout":"0s","resource_api_version":"V3"}}]},"sni":"envoy_metrics_service"}}`,
		},
		{
			testName: "envoy metrics keepalive nil",
			key:      "envoy_metrics_service_tcp_keepalive",
			option:   option.EnvoyMetricsServiceTCPKeepalive(nil),
			expected: nil,
		},
		{
			testName: "envoy metrics keepalive",
			key:      "envoy_metrics_service_tcp_keepalive",
			option: option.EnvoyMetricsServiceTCPKeepalive(&networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
				Time: durationpb.New(time.Second),
			}),
			expected: "{\"tcp_keepalive\":{\"keepalive_time\":{\"value\":1}}}",
		},
		{
			testName: "envoy accesslog address empty",
			key:      "envoy_accesslog_service_address",
			option:   option.EnvoyAccessLogServiceAddress(""),
			expected: nil,
		},
		{
			testName: "envoy accesslog address ipv4",
			key:      "envoy_accesslog_service_address",
			option:   option.EnvoyAccessLogServiceAddress("127.0.0.1:80"),
			expected: "{\"address\": \"127.0.0.1\", \"port_value\": 80}",
		},
		{
			testName: "envoy accesslog address ipv6",
			key:      "envoy_accesslog_service_address",
			option:   option.EnvoyAccessLogServiceAddress("[2001:db8::100]:80"),
			expected: "{\"address\": \"2001:db8::100\", \"port_value\": 80}",
		},
		{
			testName:    "envoy accesslog address ipv6 missing brackets",
			key:         "envoy_accesslog_service_address",
			option:      option.EnvoyAccessLogServiceAddress("2001:db8::100:80"),
			expectError: true,
		},
		{
			testName: "envoy accesslog address host port",
			key:      "envoy_accesslog_service_address",
			option:   option.EnvoyAccessLogServiceAddress("fake:80"),
			expected: "{\"address\": \"fake\", \"port_value\": 80}",
		},
		{
			testName: "envoy accesslog address no port",
			key:      "envoy_accesslog_service_address",
			option:   option.EnvoyAccessLogServiceAddress("fake:"),
			expected: "{\"address\": \"fake\", \"port_value\": }",
		},
		{
			testName:    "envoy accesslog address missing port",
			key:         "envoy_accesslog_service_address",
			option:      option.EnvoyAccessLogServiceAddress("127.0.0.1"),
			expectError: true,
		},
		{
			testName: "envoy accesslog tls nil",
			key:      "envoy_accesslog_service_tls",
			option:   option.EnvoyAccessLogServiceTLS(nil, &model.BootstrapNodeMetadata{}),
			expected: nil,
		},
		{
			testName: "envoy access log tls",
			key:      "envoy_accesslog_service_tls",
			option: option.EnvoyAccessLogServiceTLS(&networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
			}, &model.BootstrapNodeMetadata{}),
			expected: `{"name":"envoy.transport_sockets.tls","typed_config":{"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext","common_tls_context":{"alpn_protocols":["istio","h2"],"combined_validation_context":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"ROOTCA","sds_config":{"api_config_source":{"api_type":"GRPC","grpc_services":[{"envoy_grpc":{"cluster_name":"sds-grpc"}}],"set_node_on_first_message_only":true,"transport_api_version":"V3"},"initial_fetch_timeout":"0s","resource_api_version":"V3"}}},"tls_certificate_sds_secret_configs":[{"name":"default","sds_config":{"api_config_source":{"api_type":"GRPC","grpc_services":[{"envoy_grpc":{"cluster_name":"sds-grpc"}}],"set_node_on_first_message_only":true,"transport_api_version":"V3"},"initial_fetch_timeout":"0s","resource_api_version":"V3"}}]},"sni":"envoy_accesslog_service"}}`,
		},
		{
			testName: "envoy access log keepalive nil",
			key:      "envoy_accesslog_service_tcp_keepalive",
			option:   option.EnvoyAccessLogServiceTCPKeepalive(nil),
			expected: nil,
		},
		{
			testName: "envoy access log keepalive",
			key:      "envoy_accesslog_service_tcp_keepalive",
			option: option.EnvoyAccessLogServiceTCPKeepalive(&networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive{
				Time: durationpb.New(time.Second),
			}),
			expected: "{\"tcp_keepalive\":{\"keepalive_time\":{\"value\":1}}}",
		},
		{
			testName: "envoy stats matcher inclusion prefix nil",
			key:      "inclusionPrefix",
			option:   option.EnvoyStatsMatcherInclusionPrefix(nil),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion prefix empty",
			key:      "inclusionPrefix",
			option:   option.EnvoyStatsMatcherInclusionPrefix(make([]string, 0)),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion prefix",
			key:      "inclusionPrefix",
			option:   option.EnvoyStatsMatcherInclusionPrefix([]string{"fake"}),
			expected: []string{"fake"},
		},
		{
			testName: "envoy stats matcher inclusion suffix nil",
			key:      "inclusionSuffix",
			option:   option.EnvoyStatsMatcherInclusionSuffix(nil),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion suffix empty",
			key:      "inclusionSuffix",
			option:   option.EnvoyStatsMatcherInclusionSuffix(make([]string, 0)),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion suffix",
			key:      "inclusionSuffix",
			option:   option.EnvoyStatsMatcherInclusionSuffix([]string{"fake"}),
			expected: []string{"fake"},
		},
		{
			testName: "envoy stats matcher inclusion regexp nil",
			key:      "inclusionRegexps",
			option:   option.EnvoyStatsMatcherInclusionRegexp(nil),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion regexp empty",
			key:      "inclusionRegexps",
			option:   option.EnvoyStatsMatcherInclusionRegexp(make([]string, 0)),
			expected: nil,
		},
		{
			testName: "envoy stats matcher inclusion regexp",
			key:      "inclusionRegexps",
			option:   option.EnvoyStatsMatcherInclusionRegexp([]string{"fake"}),
			expected: []string{"fake"},
		},
		{
			testName: "sts enabled",
			key:      "sts",
			option:   option.STSEnabled(true),
			expected: true,
		},
		{
			testName: "sts port",
			key:      "sts_port",
			option:   option.STSPort(5555),
			expected: 5555,
		},
		{
			testName: "project id",
			key:      "gcp_project_id",
			option:   option.GCPProjectID("project"),
			expected: "project",
		},
		{
			testName: "tracing tls nil",
			key:      "tracing_tls",
			option:   option.TracingTLS(nil, &model.BootstrapNodeMetadata{}, false),
			expected: nil,
		},
		{
			testName: "tracing tls",
			key:      "tracing_tls",
			option: option.TracingTLS(&networkingAPI.ClientTLSSettings{
				Mode:           networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "/etc/tracing/ca.pem",
			}, &model.BootstrapNodeMetadata{}, false),
			expected: `{"name":"envoy.transport_sockets.tls","typed_config":{"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext","common_tls_context":{"combined_validation_context":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"file-root:/etc/tracing/ca.pem","sds_config":{"api_config_source":{"api_type":"GRPC","grpc_services":[{"envoy_grpc":{"cluster_name":"sds-grpc"}}],"set_node_on_first_message_only":true,"transport_api_version":"V3"},"resource_api_version":"V3"}}}}}}`,
		},
	}

	for _, c := range cases {
		t.Run(c.testName, func(t *testing.T) {
			g := NewWithT(t)

			params, err := option.NewTemplateParams(c.option)
			if c.expectError {
				g.Expect(err).ToNot(BeNil())
			} else {
				g.Expect(err).To(BeNil())
				actual, ok := params[c.key]
				if c.expected == nil {
					g.Expect(ok).To(BeFalse())
				} else {
					g.Expect(ok).To(BeTrue())
					g.Expect(actual).To(Equal(c.expected))
				}
			}
		})
	}
}
