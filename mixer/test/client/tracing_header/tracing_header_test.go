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
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

const envoyConf = `
admin:
  access_log_path: {{.AccessLogPath}}
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.Ports.AdminPort}}
tracing:
  http:
    name: envoy.zipkin
    config:
      collector_cluster: zipkin
      collector_endpoint_version: HTTP_JSON
      collector_endpoint: /
      trace_id_128bit: true
      shared_span_context: false
static_resources:
  clusters:
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
    circuit_breakers:
      thresholds:
      - max_connections: 10000
        max_pending_requests: 10000
        max_requests: 10000
        max_retries: 3
  - name: zipkin
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: 9411
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
{{.FiltersBeforeMixer | indent 10 }}
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
          tracing:
            operation_name: INGRESS
            overall_sampling:
              value: 100.0
            random_sampling:
              value: 100.0
            client_sampling:
              value: 100.0
`

// Report attributes from a good GET request
const reportAttributesOkGet = `
{
  "check.cache_hit": "false",
  "connection.mtls": "false",
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "destination.ip": "[127 0 0 1]",
  "destination.namespace": "",
  "destination.port": "20309",
  "destination.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "origin.ip": "[127 0 0 1]",
  "quota.cache_hit": "false",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*",
     "x-b3-traceid": "*",
     "x-b3-spanid": "*",
     "x-b3-sampled": "*" 
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

func TestTracingHeaders(t *testing.T) {
	s := env.NewTestSetup(env.TracingHeaderTest, t)
	s.EnvoyTemplate = envoyConf

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// Issues a GET echo request with 0 size body
	if _, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)); err != nil {
		t.Errorf("Failed in request: %v", err)
	}
	// Verifies the report request has tracing headers
	s.VerifyReport("http", reportAttributesOkGet)
}
