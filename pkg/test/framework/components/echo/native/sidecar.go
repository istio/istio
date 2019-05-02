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

package native

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"testing"
	"text/template"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/util/reserveport"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceNodeSeparator = "~"
	serviceCluster       = "local"
	proxyType            = "sidecar"

	envoyBootstrapYAML = `
{{- $serviceName := .ServiceName -}}
stats_config:
  use_all_default_tags: false
node:
  id: {{ .NodeID }}
  cluster: {{ .Cluster }}
admin:
  access_log_path: "/dev/null"
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.AdminPort}}
dynamic_resources:
  lds_config:
    ads: {}
  cds_config:
    ads: {}
  ads_config:
    api_type: GRPC
    refresh_delay: 1s
    grpc_services:
    - envoy_grpc:
        cluster_name: xds-grpc
static_resources:
  clusters:
  - name: xds-grpc
    type: STATIC
    connect_timeout: 1s
    lb_policy: ROUND_ROBIN
    http2_protocol_options: {}
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{.DiscoveryPort}}
  {{ range $i, $p := .Ports -}}
  - name: service_{{$serviceName}}_{{$p.InstancePort}}
    connect_timeout: 0.25s
    type: STATIC
    lb_policy: ROUND_ROBIN
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{$p.InstancePort}}    
  {{ end -}}
  listeners:
  {{- range $i, $p := .Ports }}
  - address:
      socket_address:
        address: 127.0.0.1
        port_value: {{$p.ServicePort}}
    use_original_dst: true
    filter_chains:
    - filters:
      {{- if $p.Protocol.IsHTTP }}
      - name: envoy.http_connection_manager
        config:
          codec_type: auto
          {{- if $p.Protocol.IsHTTP2 }}
          http2_protocol_options:
            max_concurrent_streams: 1073741824
          {{- end }}
          stat_prefix: ingress_http
          route_config:
            name: service_{{$serviceName}}_{{$p.ServicePort}}_to_{{$p.InstancePort}}
            virtual_hosts:
            - name: service_{{$serviceName}}
              domains:
              - "*"
              routes:
              - match:
                  prefix: "/"
                route:
                  cluster: service_{{$serviceName}}_{{$p.InstancePort}}
          http_filters:
          - name: envoy.cors
            config: {}
          - name: envoy.fault
            config: {}
          - name: envoy.router
            config: {}
      {{- else }}
      - name: envoy.tcp_proxy
        config:
          stat_prefix: ingress_tcp
          cluster: service_{{$serviceName}}_{{$p.InstancePort}}
      {{- end }}
  {{- end -}}
`
)

var (
	envoyBootstrapTemplate *template.Template

	_ echo.Sidecar = &sidecar{}
)

func init() {
	envoyBootstrapTemplate = template.New("istio_agent_envoy_config")
	_, err := envoyBootstrapTemplate.Parse(envoyBootstrapYAML)
	if err != nil {
		panic("unable to parse Envoy bootstrap config")
	}
}

type sidecarConfig struct {
	service          string
	namespace        string
	domain           string
	outDir           string
	servicePorts     model.PortList
	portManager      reserveport.PortManager
	discoveryAddress *net.TCPAddr
	envoyLogLevel    envoy.LogLevel
}

func newSidecar(cfg sidecarConfig) (*sidecar, error) {
	// Generate the port mappings between Envoy and the backend service.
	adminPort, mappedPorts, err := createEnvoyPorts(cfg.portManager, cfg.servicePorts)
	if err != nil {
		return nil, err
	}

	nodeID := generateEnvoyServiceNode(cfg.service, cfg.namespace, cfg.domain)

	// Create the YAML configuration file for Envoy.
	yamlFile, err := createEnvoyBootstrapFile(cfg.outDir, cfg.service, nodeID,
		adminPort, cfg.discoveryAddress.Port, mappedPorts)
	if err != nil {
		return nil, err
	}

	// Start Envoy with the configuration
	e := &envoy.Envoy{
		YamlFile:       yamlFile,
		LogLevel:       cfg.envoyLogLevel,
		LogEntryPrefix: fmt.Sprintf("[ENVOY-%s.%s.%s]", cfg.service, cfg.namespace, cfg.domain),
	}
	if err = e.Start(); err != nil {
		return nil, err
	}

	// Wait for Envoy to become healthy.
	if err = envoy.WaitForHealthCheckLive(adminPort); err != nil {
		_ = e.Stop()
		return nil, err
	}

	return &sidecar{
		adminPort: adminPort,
		ports:     mappedPorts,
		nodeID:    nodeID,
		yamlFile:  yamlFile,
		envoy:     e,
	}, nil
}

type sidecar struct {
	adminPort int
	ports     []echo.Port
	nodeID    string
	yamlFile  string
	envoy     *envoy.Envoy
}

func (s *sidecar) GetPorts() []echo.Port {
	return s.ports
}

// FindFirstPortForProtocol is a utility method to simplify lookup of a port for a given protocol.
func (s *sidecar) FindFirstPortForProtocol(protocol model.Protocol) (echo.Port, error) {
	for _, port := range s.GetPorts() {
		if port.Protocol == protocol {
			return port, nil
		}
	}
	return echo.Port{}, fmt.Errorf("no port found matching protocol %v", protocol)
}

func (s *sidecar) NodeID() string {
	return s.nodeID
}

func (s *sidecar) Info() (*envoyAdmin.ServerInfo, error) {
	return envoy.GetServerInfo(s.adminPort)
}

func (s *sidecar) InfoOrFail(t testing.TB) *envoyAdmin.ServerInfo {
	info, err := s.Info()
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func (s *sidecar) Config() (*envoyAdmin.ConfigDump, error) {
	return envoy.GetConfigDump(s.adminPort)
}

func (s *sidecar) ConfigOrFail(t testing.TB) *envoyAdmin.ConfigDump {
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (s *sidecar) WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error {
	return common.WaitForConfig(s.Config, accept, options...)
}

func (s *sidecar) WaitForConfigOrFail(t testing.TB, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) {
	if err := s.WaitForConfig(accept, options...); err != nil {
		t.Fatal(err)
	}
}

func (s *sidecar) Stop() {
	_ = s.envoy.Stop()
}

func generateEnvoyServiceNode(serviceName, namespace, domain string) string {
	id := fmt.Sprintf("%s.%s", serviceName, randomBase64String(10))
	return strings.Join([]string{proxyType, localhost, id, namespace + "." + domain}, serviceNodeSeparator)
}

func createEnvoyPorts(portManager reserveport.PortManager, servicePorts model.PortList) (adminPort int, mappedPorts []echo.Port, err error) {
	if adminPort, err = findFreePort(portManager); err != nil {
		return
	}

	mappedPorts = make([]echo.Port, len(servicePorts))
	for i, servicePort := range servicePorts {
		var envoyPort int
		envoyPort, err = findFreePort(portManager)
		if err != nil {
			return
		}

		mappedPorts[i] = echo.Port{
			Name:         servicePort.Name,
			Protocol:     servicePort.Protocol,
			ServicePort:  envoyPort,
			InstancePort: servicePort.Port,
		}
	}
	return
}

func createEnvoyBootstrapFile(outDir, serviceName, nodeID string, adminPort, discoveryPort int, ports []echo.Port) (string, error) {
	// Create an output file to hold the generated configuration.
	yamlFile, err := createTempFile(outDir, "envoy_bootstrap", ".yaml")
	if err != nil {
		return "", err
	}

	// Apply the template with the current configuration
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := envoyBootstrapTemplate.Execute(w, map[string]interface{}{
		"ServiceName":   serviceName,
		"NodeID":        nodeID,
		"Cluster":       serviceCluster,
		"AdminPort":     adminPort,
		"Ports":         ports,
		"DiscoveryPort": discoveryPort,
	}); err != nil {
		return "", err
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	configBytes := filled.Bytes()

	// Write the content of the file.
	if err := ioutil.WriteFile(yamlFile, configBytes, 0644); err != nil {
		return "", err
	}
	return yamlFile, nil
}
