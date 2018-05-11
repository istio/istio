//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"

	"istio.io/istio/pkg/log"
)

const (
	proxyYamlTemplateStr = `
{{- $serviceName := .ServiceName -}}
admin:
  access_log_path: "/dev/null"
  address:
    socket_address:
      address: 127.0.0.1
      port_value: {{.AdminPort}}
static_resources:
  clusters:
  {{ range $i, $p := .Ports -}}
  - name: {{$serviceName}}_{{$p.ServicePort}}
    connect_timeout: 0.25s
    type: STATIC
    lb_policy: ROUND_ROBIN
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: {{$p.ServicePort}}    
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
      - name: envoy.http_connection_manager
        config:
          codec_type: auto
          stat_prefix: ingress_http
          route_config:
            name: {{$p.ProxyPort}}_to_{{$serviceName}}_{{$p.ServicePort}}
            virtual_hosts:
            - name: {{$serviceName}}
              domains:
              - "*"
              routes:
              - match:
                  prefix: "/"
                route:
                  cluster: {{$serviceName}}_{{$p.ServicePort}}
          http_filters:
          - name: envoy.cors
            config: {}
          - name: envoy.router
            config: {}
  {{- end -}}
`
)

var (
	// The Template object parsed from the template string
	proxyYamlTemplate = getProxyYamlTemplate()
)

func getProxyYamlTemplate() *template.Template {
	tmpl := template.New("istio_agent_proxy_config")
	_, err := tmpl.Parse(proxyYamlTemplateStr)
	if err != nil {
		log.Warn("unable to parse proxy bootstrap config")
	}
	return tmpl
}

type proxyConfigBuilder struct {
	ServiceName string
	AdminPort   int
	Ports       []Port
	tmpDir      string
}

func (b *proxyConfigBuilder) build() (*proxyConfig, error) {
	// Apply the template with the current configuration
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := proxyYamlTemplate.Execute(w, b); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}

	cfg := &proxyConfig{}

	// Create a temporary output directory if not provided.
	outDir := b.tmpDir
	if outDir == "" {
		var err error
		outDir, err = createTempDir()
		if err != nil {
			return nil, err
		}
		cfg.ownedDir = outDir
	}

	// Create an output file to hold the generated configuration.
	yamlFile, err := createTempfile(outDir, "istio_agent_proxy_config", ".yaml")
	if err != nil {
		cfg.dispose()
		return nil, err
	}
	cfg.yamlFile = yamlFile

	// Write the content of the file.
	configBytes := filled.Bytes()
	fmt.Println("NM: Envoy config:")
	fmt.Println(string(configBytes))
	if err := ioutil.WriteFile(yamlFile, configBytes, 0644); err != nil {
		cfg.dispose()
		return nil, err
	}
	return cfg, nil
}

type proxyConfig struct {
	ownedDir string
	yamlFile string
}

func (c *proxyConfig) dispose() {
	if c.ownedDir != "" {
		os.RemoveAll(c.ownedDir)
	} else {
		os.Remove(c.yamlFile)
	}
}

func createTempDir() (string, error) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "istio_agent_test")
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}

func createTempfile(tmpDir, prefix, suffix string) (string, error) {
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
