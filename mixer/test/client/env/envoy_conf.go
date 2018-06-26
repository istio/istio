// Copyright 2017 Istio Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package env

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
)

type confParam struct {
	ClientPort   uint16
	ServerPort   uint16
	TCPProxyPort uint16
	AdminPort    uint16
	MixerPort    uint16
	BackendPort  uint16

	ClientConfig       string
	ServerConfig       string
	TCPServerConfig    string
	MixerRouteConfig   string
	FiltersBeforeMixer string

	// Ports contains the allocated ports.
	Ports     *Ports
	IstioSrc  string
	IstioOut  string
	AccessLog string

	// Options are additional config options for the template
	Options map[string]interface{}
}

const envoyConfTemplYAML = `
admin:
  access_log_path: {{.AccessLog}}
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.AdminPort}}
static_resources:
  clusters:
  - name: backend
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.BackendPort}}
  - name: loop
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.ServerPort}}
  - name: mixer_server
    http2_protocol_options: {}
    connect_timeout: 5s
    type: STATIC
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.MixerPort}}
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
        port_value: {{.ServerPort}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: AUTO
          stat_prefix: inbound_http
          access_log:
          - name: envoy.file_access_log
            config:
              path: {{.AccessLog}}
          http_filters:
{{.FiltersBeforeMixer}}
          - name: mixer
            config: {{.ServerConfig}}
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
                  mixer: {{.MixerRouteConfig}}
  - name: client
    address:
      socket_address:
        address: 127.0.0.1
        port_value: {{.ClientPort}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: AUTO
          stat_prefix: outbound_http
          access_log:
          - name: envoy.file_access_log
            config:
              path: {{.AccessLog}}
          http_filters:
          - name: mixer
            config: {{.ClientConfig}}
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
        port_value: {{.TCPProxyPort}}
    filter_chains:
    - filters:
      - name: mixer
        config: {{.TCPServerConfig}}
      - name: envoy.tcp_proxy
        config:
          stat_prefix: inbound_tcp
          cluster: backend
`

func (c *confParam) write(outPath, confTmpl string) error {
	tmpl, err := template.New("test").Parse(confTmpl)
	if err != nil {
		return fmt.Errorf("failed to parse config template: %v", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create file %v: %v", outPath, err)
	}
	defer func() {
		_ = f.Close()
	}()
	return tmpl.Execute(f, *c)
}

// CreateEnvoyConf create envoy config.
func (s *TestSetup) CreateEnvoyConf(path string, stress bool, filtersBeforeMixer string, mfConfig *MixerFilterConf, ports *Ports) error {
	c := &confParam{
		ClientPort:       ports.ClientProxyPort,
		ServerPort:       ports.ServerProxyPort,
		TCPProxyPort:     ports.TCPProxyPort,
		AdminPort:        ports.AdminPort,
		MixerPort:        ports.MixerPort,
		BackendPort:      ports.BackendPort,
		AccessLog:        s.AccessLogPath,
		ServerConfig:     toJSON(mfConfig.HTTPServerConf),
		ClientConfig:     toJSON(mfConfig.HTTPClientConf),
		TCPServerConfig:  toJSON(mfConfig.TCPServerConf),
		MixerRouteConfig: toJSON(mfConfig.PerRouteConf),
		Ports:            ports,
		IstioSrc:         s.IstioSrc,
		IstioOut:         s.IstioOut,
		Options:          s.EnvoyConfigOpt,
	}
	// TODO: use fields from s directly instead of copying

	if stress {
		c.AccessLog = "/dev/null"
	}
	if len(filtersBeforeMixer) > 0 {
		pad := strings.Repeat(" ", 10)
		c.FiltersBeforeMixer = pad + strings.Replace(filtersBeforeMixer, "\n", "\n"+pad, -1)
	}

	confTmpl := envoyConfTemplYAML
	if s.EnvoyTemplate != "" {
		confTmpl = s.EnvoyTemplate
	}
	return c.write(path, confTmpl)
}

func toJSON(mixerFilterConfig proto.Message) string {
	m := jsonpb.Marshaler{OrigName: true}
	str, err := m.MarshalToString(mixerFilterConfig)
	if err != nil {
		return ""
	}
	return str
}
