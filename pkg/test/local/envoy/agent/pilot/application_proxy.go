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

package pilot

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/local/envoy"
	"istio.io/istio/pkg/test/local/envoy/agent"
)

const (
	serviceNodeSeparator = "~"
	serviceCluster       = "local"
	proxyType            = "sidecar"
	localIPAddress       = "127.0.0.1"
	localCIDR            = "127.0.0.1/8"
	// TODO(nmittler): Pilot seems to require that the domain have multiple dot-separated parts
	defaultDomain    = "svc.local"
	defaultNamespace = "default"

	envoyYamlTemplateStr = `
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
    cluster_names:
    - xds-grpc
static_resources:
  clusters:
  - name: xds-grpc
    type: STATIC
    connect_timeout: 1s
    lb_policy: ROUND_ROBIN
    http2_protocol_options: {}
    hosts:
    - socket_address:
        address: {{.DiscoveryIPAddress}}
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
      - name: envoy.http_connection_manager
        config:
          codec_type: auto
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
          - name: envoy.router
            config: {}
  {{- end -}}
`
)

var (
	// The Template object parsed from the template string
	envoyYamlTemplate = getEnvoyYamlTemplate()
)

func getEnvoyYamlTemplate() *template.Template {
	tmpl := template.New("istio_agent_proxy_config")
	_, err := tmpl.Parse(envoyYamlTemplateStr)
	if err != nil {
		log.Warn("unable to parse proxy bootstrap config")
	}
	return tmpl
}

// Factory is responsible for manufacturing proxy instances which use Pilot for configuration.
type Factory struct {
	Namespace        string
	Domain           string
	DiscoveryAddress *net.TCPAddr
	TmpDir           string
}

// NewProxiedApplication is an agent.ApplicationProxyFactory function that creates new proxy instances which use Pilot for configuration
func (f *Factory) NewProxiedApplication(serviceName string, app agent.Application) (agent.ApplicationProxy, agent.StopFunc, error) {
	var err error

	proxy := &applicationProxy{}
	stopFunc := proxy.stop
	defer func() {
		if err != nil {
			_ = stopFunc()
		}
	}()

	if err = proxy.start(serviceName, app, f); err != nil {
		return nil, nil, err
	}

	return proxy, stopFunc, nil
}

func (f *Factory) getNamespace() string {
	if f.Namespace != "" {
		return f.Namespace
	}
	return defaultNamespace
}

func (f *Factory) getDomain() string {
	if f.Domain != "" {
		return f.Domain
	}
	return defaultDomain
}

func (f *Factory) getFQD() string {
	return fmt.Sprintf("%s.%s", f.getNamespace(), f.getDomain())
}

func (f *Factory) generateServiceNode(serviceName string) string {
	id := fmt.Sprintf("%s.%s", serviceName, randomBase64String(10))
	return strings.Join([]string{proxyType, localIPAddress, id, f.getFQD()}, serviceNodeSeparator)
}

type applicationProxy struct {
	filter       *discoveryFilter
	envoy        *envoy.Envoy
	adminPort    int
	ports        []agent.MappedPort
	yamlFile     string
	ownedDir     string
	serviceEntry model.Config
}

// GetConfig implements the agent.ApplicationProxy interface.
func (p *applicationProxy) GetConfig() model.Config {
	return p.serviceEntry
}

// GetPorts implements the agent.ApplicationProxy interface.
func (p *applicationProxy) GetAdminPort() int {
	return p.adminPort
}

// GetPorts implements the agent.ApplicationProxy interface.
func (p *applicationProxy) GetPorts() []agent.MappedPort {
	return p.ports
}

func (p *applicationProxy) start(serviceName string, app agent.Application, f *Factory) error {
	// Start a discovery pilot that will sit between Envoy and Pilot.
	p.filter = &discoveryFilter{}
	err := p.filter.start(f.DiscoveryAddress.String())
	if err != nil {
		return err
	}

	// Generate the port mappings between Envoy and the backend service.
	p.adminPort, p.ports, err = createPorts(app.GetPorts())
	if err != nil {
		return err
	}

	nodeID := f.generateServiceNode(serviceName)

	// Create the YAML configuration file for Envoy.
	if err = p.createYamlFile(serviceName, nodeID, f); err != nil {
		return err
	}

	// Start Envoy with the configuration
	logPrefix := fmt.Sprintf("[ENVOY-%s]", nodeID)
	p.envoy = &envoy.Envoy{
		YamlFile:       p.yamlFile,
		LogEntryPrefix: logPrefix,
	}
	if err = p.envoy.Start(); err != nil {
		return err
	}

	// Wait for Envoy to become healthy.
	if err = envoy.WaitForHealthCheckLive(p.adminPort); err != nil {
		return err
	}

	// Now create the service entry for this service.
	p.serviceEntry = model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      serviceName,
			Namespace: f.getNamespace(),
			Domain:    f.getDomain(),
			Type:      model.ServiceEntry.Type,
		},
		Spec: &v1alpha3.ServiceEntry{
			Hosts: []string{
				fmt.Sprintf("%s.%s", serviceName, f.getFQD()),
			},
			Addresses: []string{
				localCIDR,
			},
			Resolution: v1alpha3.ServiceEntry_STATIC,
			Ports:      p.getConfigPorts(),
			Location:   v1alpha3.ServiceEntry_MESH_INTERNAL,
			Endpoints: []*v1alpha3.ServiceEntry_Endpoint{
				{
					Address: localIPAddress,
				},
			},
		},
	}

	return err
}

func (p *applicationProxy) stop() (err error) {
	if p.filter != nil {
		p.filter.stop()
	}
	if p.envoy != nil {
		err = p.envoy.Stop()
	}
	if p.ownedDir != "" {
		_ = os.RemoveAll(p.ownedDir)
	} else if p.yamlFile != "" {
		_ = os.Remove(p.yamlFile)
	}
	return
}

func (p *applicationProxy) getConfigPorts() []*v1alpha3.Port {
	ports := make([]*v1alpha3.Port, len(p.ports))
	for i, p := range p.ports {
		ports[i] = &v1alpha3.Port{
			Name:     p.Name,
			Protocol: string(p.Protocol),
			Number:   uint32(p.ProxyPort),
		}
	}
	return ports
}

func (p *applicationProxy) createYamlFile(serviceName, nodeID string, f *Factory) error {
	// Create a temporary output directory if not provided.
	outDir := f.TmpDir
	if outDir == "" {
		var err error
		p.ownedDir, err = createTempDir()
		if err != nil {
			return err
		}
	}

	// Create an output file to hold the generated configuration.
	var err error
	p.yamlFile, err = createTempfile(outDir, "istio_agent_proxy_config", ".yaml")
	if err != nil {
		return err
	}

	// Apply the template with the current configuration
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := envoyYamlTemplate.Execute(w, map[string]interface{}{
		"ServiceName":        serviceName,
		"NodeID":             nodeID,
		"Cluster":            serviceCluster,
		"AdminPort":          p.adminPort,
		"Ports":              p.ports,
		"DiscoveryIPAddress": localIPAddress,
		"DiscoveryPort":      f.DiscoveryAddress.Port,
	}); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	// Write the content of the file.
	configBytes := filled.Bytes()
	if err := ioutil.WriteFile(p.yamlFile, configBytes, 0644); err != nil {
		return err
	}
	return nil
}

func randomBase64String(len int) string {
	buff := make([]byte, len)
	rand.Read(buff)
	str := base64.URLEncoding.EncodeToString(buff)
	return str[:len]
}

func createPorts(servicePorts model.PortList) (adminPort int, ports []agent.MappedPort, err error) {
	if adminPort, err = findFreePort(); err != nil {
		return
	}

	ports = make([]agent.MappedPort, len(servicePorts))
	for i, servicePort := range servicePorts {
		var envoyPort int
		envoyPort, err = findFreePort()
		if err != nil {
			return
		}

		ports[i] = agent.MappedPort{
			Name:            servicePort.Name,
			Protocol:        servicePort.Protocol,
			ApplicationPort: servicePort.Port,
			ProxyPort:       envoyPort,
		}
	}
	return
}

func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
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
