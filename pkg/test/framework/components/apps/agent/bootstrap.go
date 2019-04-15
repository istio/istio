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

package agent

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"
)

const (
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
  - name: service_{{$serviceName}}_{{$p.ApplicationPort}}
    connect_timeout: 0.25s
    type: STATIC
    lb_policy: ROUND_ROBIN
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{$p.ApplicationPort}}    
  {{ end -}}
  listeners:
  {{- range $i, $p := .Ports }}
  - address:
      socket_address:
        address: 127.0.0.1
        port_value: {{$p.ProxyPort}}
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
            name: service_{{$serviceName}}_{{$p.ProxyPort}}_to_{{$p.ApplicationPort}}
            virtual_hosts:
            - name: service_{{$serviceName}}
              domains:
              - "*"
              routes:
              - match:
                  prefix: "/"
                route:
                  cluster: service_{{$serviceName}}_{{$p.ApplicationPort}}
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
          cluster: service_{{$serviceName}}_{{$p.ApplicationPort}}
      {{- end }}
  {{- end -}}
`
)

var (
	envoyBootstrapTemplate = getEnvoyBootstrapTemplate()
)

func getEnvoyBootstrapTemplate() *template.Template {
	tmpl := template.New("istio_agent_envoy_config")
	_, err := tmpl.Parse(envoyBootstrapYAML)
	if err != nil {
		panic("unable to parse Envoy bootstrap config")
	}
	return tmpl
}

func createEnvoyBootstrapFile(outDir, serviceName, nodeID string, adminPort, discoveryPort int, ports []*MappedPort) (string, error) {
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

func createTempFile(tmpDir, prefix, suffix string) (string, error) {
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		return "", err
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(tmpName); err != nil {
		return "", err
	}
	return tmpName + suffix, nil
}
