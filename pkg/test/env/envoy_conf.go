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

package env

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
)

const envoyConfTemplYAML = `
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
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.BackendPort}}
  - name: loop
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.ServerProxyPort}}
  - name: extra_server
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.ExtraPort}}
    circuit_breakers:
      thresholds:
      - max_connections: 10000
        max_pending_requests: 10000
        max_requests: 10000
        max_retries: 3
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
  - name: client
    address:
      socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.ClientProxyPort}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: AUTO
          stat_prefix: outbound_http
          access_log:
          - name: envoy.file_access_log
            config:
              path: {{.AccessLogPath}}
          http_filters:
          - name: envoy.router
          route_config:
            name: loop
            virtual_hosts:
            - name: loop
              domains: ["*"]
              routes:
              - match:
                  prefix: /
                route:
                  cluster: loop
                  timeout: 0s
  - name: tcp_server
    address:
      socket_address:
        address: 127.0.0.1
        port_value: {{.Ports.TCPProxyPort}}
    filter_chains:
    - filters:
      - name: envoy.tcp_proxy
        config:
          stat_prefix: inbound_tcp
          cluster: backend
`

// CreateEnvoyConf create envoy config.
func (s *TestSetup) CreateEnvoyConf(path string) error {
	if s.stress {
		s.AccessLogPath = "/dev/null"
	}

	confTmpl := envoyConfTemplYAML
	if s.EnvoyTemplate != "" {
		confTmpl = s.EnvoyTemplate
	}

	tmpl, err := template.New("test").Funcs(template.FuncMap{
		"toJSON": toJSON,
		"indent": indent,
	}).Parse(confTmpl)
	if err != nil {
		return fmt.Errorf("failed to parse config template: %v", err)
	}
	tmpl.Funcs(template.FuncMap{})

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file %v: %v", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	return tmpl.Execute(f, s)
}

func toJSON(mixerFilterConfig proto.Message) string {
	m := jsonpb.Marshaler{OrigName: true}
	str, err := m.MarshalToString(mixerFilterConfig)
	if err != nil {
		return ""
	}
	return str
}

func indent(n int, s string) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.Replace(s, "\n", "\n"+pad, -1)
}
