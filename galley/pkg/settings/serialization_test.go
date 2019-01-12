//  Copyright 2019 Istio Authors
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

package settings

import (
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestToYaml_Error(t *testing.T) {
	_, err := ToYaml(nil)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestParse_Error(t *testing.T) {
	_, err := Parse("!AAA")
	if err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestParse_Error2(t *testing.T) {
	_, err := Parse(`
	g
`)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestParseFileOrDefault_NoFile(t *testing.T) {
	g, err := ParseFileOrDefault("./notexists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(g, Default()) {
		t.Fatalf("Mismatch. got:\n%v\nwanted:\n%v\n", g, Default())
	}
}

func TestParseFileOrDefault_BadFile(t *testing.T) {
	_, err := ParseFileOrDefault(os.TempDir())
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestParseFileOrDefault_BadFileContents(t *testing.T) {
	f, err := ioutil.TempFile(os.TempDir(), "TestParseFileOrDefault")
	if err != nil {
		t.Fatalf("Error creating temp file: %v", err)
	}

	if _, err = f.WriteString("!!!!!!"); err != nil {
		t.Fatalf("Error writing to file: %v", err)
	}

	_, err = ParseFileOrDefault(f.Name())
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestRoundtrip(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:  "default",
			input: ``,
			expected: `
general:
  introspection:
    address: 127.0.0.1
    port: 9876
  liveness:
    interval: 1s
    path: /healthLiveness
  meshConfigFile: /etc/mesh-config/mesh
  monitoringPort: 9093
  pprofPort: 9094
  readiness:
    interval: 1s
    path: /healthReadiness
processing:
  domainSuffix: cluster.local
  server:
    address: tcp://0.0.0.0:9901
    auth:
      mtls:
        caCertificates: /etc/certs/root-cert.pem
        clientCertificate: /etc/certs/cert-chain.pem
        privateKey: /etc/certs/key.pem
    maxConcurrentStreams: 1024
    maxReceivedMessageSize: 1048576
  source:
    kubernetes:
      resyncPeriod: 0s
validation:
  deploymentName: istio-galley
  deploymentNamespace: istio-system
  serviceName: istio-galley
  tls:
    caCertificates: /etc/certs/root-cert.pem
    clientCertificate: /etc/certs/cert-chain.pem
    privateKey: /etc/certs/key.pem
  webhookConfigFile: /etc/config/validatingwebhookconfiguration.yaml
  webhookName: istio-galley
  webhookPort: 443
 
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse(test.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			actual, err := ToYaml(cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if strings.TrimSpace(actual) != strings.TrimSpace(test.expected) {
				t.Fatalf("Mismatch: got:\n%v\nwanted:\n%v\n", actual, test.expected)
			}
		})
	}
}
