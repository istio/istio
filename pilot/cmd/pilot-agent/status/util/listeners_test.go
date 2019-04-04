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

package util_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/util/reserveport"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	envoyBootstrapYaml = `
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
static_resources:
  clusters:
  - name: fakeCluster
    type: STATIC
    connect_timeout: 1s
    lb_policy: ROUND_ROBIN
    http2_protocol_options: {}
    hosts:
    - socket_address:
        address: 127.0.0.1
        port_value: 80
  listeners:
  - address:
      socket_address:
        address: 0.0.0.0
        port_value: {{.ListenerPort1}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: auto
          stat_prefix: ingress_http
          route_config:
            name: myservice
            virtual_hosts:
            - name: myvirtualhost
              domains:
              - "*"
              routes:
              - match:
                  prefix: "/"
                route:
                  cluster: fakeCluster
          http_filters:
          - name: envoy.cors
            config: {}
          - name: envoy.fault
            config: {}
          - name: envoy.router
            config: {}
  - address:
      socket_address:
        address: 0:0:0:0:0:0:0:0
        port_value: {{.ListenerPort2}}
    filter_chains:
    - filters:
      - name: envoy.http_connection_manager
        config:
          codec_type: auto
          stat_prefix: ingress_http
          route_config:
            name: myservice
            virtual_hosts:
            - name: myvirtualhost
              domains:
              - "*"
              routes:
              - match:
                  prefix: "/"
                route:
                  cluster: fakeCluster
          http_filters:
          - name: envoy.cors
            config: {}
          - name: envoy.fault
            config: {}
          - name: envoy.router
            config: {}
`
)

var (
	envoyBootstrapTemplate = getEnvoyBootstrapTemplate()
)

func getEnvoyBootstrapTemplate() *template.Template {
	tmpl := template.New("istio_agent_envoy_config")
	_, err := tmpl.Parse(envoyBootstrapYaml)
	if err != nil {
		panic("unable to parse Envoy bootstrap config")
	}
	return tmpl
}

func TestParseInboundListeners(t *testing.T) {
	g := NewGomegaWithT(t)

	fromEnvoy := `["0.0.0.0:8080","127.0.0.1:80","[::1]:90","[::]:91","notlocal:100"]`
	out, err := util.ParseInboundListeners(fromEnvoy)
	g.Expect(err).To(BeNil())

	g.Expect(out).To(Equal(map[uint16]struct{}{
		8080: {},
		80:   {},
		90:   {},
		91:   {},
	}))
}

func TestParseRealEnvoyListeners(t *testing.T) {
	tmpDir := createTempDir(t)
	defer func() {
		_ = os.Remove(tmpDir)
	}()

	// Reserve ports for Envoy
	portMgr, err := reserveport.NewPortManager()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = portMgr.Close()
	}()
	adminPort := findFreePort(t, portMgr)
	listenerPort1 := findFreePort(t, portMgr)
	listenerPort2 := findFreePort(t, portMgr)

	// Create the bootstrap YAML file for Envoy.
	yamlFile := createYamlFile(t, tmpDir, adminPort, listenerPort1, listenerPort2)

	// Start Envoy
	e := &envoy.Envoy{
		YamlFile:       yamlFile,
		LogEntryPrefix: "[ENVOY]",
	}
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = e.Stop()
	}()

	// Fetch the listeners from Envoy.
	var buf *bytes.Buffer
	retry.UntilSuccessOrFail(t, func() (err error) {
		buf, err = util.GetListeners(uint16(adminPort))
		return
	}, retry.Timeout(10*time.Second), retry.Delay(1*time.Second))

	ports, err := util.ParseInboundListeners(buf.String())
	if err != nil {
		t.Fatal(err)
	}

	for _, listenerPort := range []int{listenerPort1, listenerPort2} {
		if _, ok := ports[uint16(listenerPort)]; !ok {
			t.Fatalf("missing entry for port: %d\nEnvoy response: %s\nParsed inbound ports: %+v", listenerPort, buf, ports)
		}
	}
}

func createYamlFile(t *testing.T, tempDir string, adminPort, listenerPort1, listenerPort2 int) string {
	// Create an output file to hold the generated configuration.
	yamlFile := createTempfile(t, tempDir, "istio_agent_envoy_config", ".yaml")

	// Apply the template with the current configuration
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := envoyBootstrapTemplate.Execute(w, map[string]interface{}{
		"NodeID":        randomBase64String(10),
		"Cluster":       randomBase64String(10),
		"AdminPort":     adminPort,
		"ListenerPort1": listenerPort1,
		"ListenerPort2": listenerPort2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	// Write the content of the file.
	configBytes := filled.Bytes()
	if err := ioutil.WriteFile(yamlFile, configBytes, 0644); err != nil {
		t.Fatal(err)
	}
	return yamlFile
}

func findFreePort(t *testing.T, manager reserveport.PortManager) int {
	reservedPort, err := manager.ReservePort()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = reservedPort.Close()
	}()

	return int(reservedPort.GetPort())
}

func randomBase64String(len int) string {
	buff := make([]byte, len)
	_, _ = rand.Read(buff)
	str := base64.URLEncoding.EncodeToString(buff)
	return str[:len]
}

func createTempDir(t *testing.T) string {
	t.Helper()
	tmpDir, err := ioutil.TempDir(os.TempDir(), "listeners_test")
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir
}

func createTempfile(t *testing.T, tmpDir, prefix, suffix string) string {
	t.Helper()
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(tmpName); err != nil {
		t.Fatal(err)
	}
	return tmpName + suffix
}
