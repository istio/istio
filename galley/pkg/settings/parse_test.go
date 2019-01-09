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
	"strconv"
	"strings"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input: ``,
			expected: `
general:
  introspection:
    address: 127.0.0.1
    port: 9876
  liveness:
    interval: 2s
    path: /healthLiveness
  meshConfigFile: /etc/mesh-config/mesh
  monitoringPort: 9093
  pprofPort: 9094
  readiness:
    interval: 2s
    path: /healthReadiness
processing:
  domainSuffix: cluster.local
  server:
    address: tcp://0.0.0.0:9901
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
    caCertificates: /etc/certs/cert-chain.pem
    clientCertificate: /etc/certs/cert-chain.pem
    privateKey: /etc/certs/key.pem
  webhookConfigFile: etc/config/validatingwebhookconfiguration.yaml
  webhookName: istio-galley
  webhookPort: 443
`,
		},
	}

	for i, test := range tests {
		t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
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
