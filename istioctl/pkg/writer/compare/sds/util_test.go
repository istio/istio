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

package sdscompare

import (
	"encoding/json"
	"io/ioutil"
	"reflect"
	"testing"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

const (
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQbfOzhcKTldFipQ1X2WXpHDANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyRhNzU5YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUw
HhcNMTkwNTE2MjIxMTI2WhcNMjQwNTE0MjMxMTI2WjAvMS0wKwYDVQQDEyRhNzU5
YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC6sSAN80Ci0DYFpNDumGYoejMQai42g6nSKYS+ekvs
E7uT+eepO74wj8o6nFMNDu58+XgIsvPbWnn+3WtUjJfyiQXxmmTg8om4uY1C7R1H
gMsrL26pUaXZ/lTE8ZV5CnQJ9XilagY4iZKeptuZkxrWgkFBD7tr652EA3hmj+3h
4sTCQ+pBJKG8BJZDNRrCoiABYBMcFLJsaKuGZkJ6KtxhQEO9QxJVaDoSvlCRGa8R
fcVyYQyXOZ+0VHZJQgaLtqGpiQmlFttpCwDiLfMkk3UAd79ovkhN1MCq+O5N7YVt
eVQWaTUqUV2tKUFvVq21Zdl4dRaq+CF5U8uOqLY/4Kg9AgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQCg
oF71Ey2b1QY22C6BXcANF1+wPzxJovFeKYAnUqwh3rF7pIYCS/adZXOKlgDBsbcS
MxAGnCRi1s+A7hMYj3sQAbBXttc31557lRoJrx58IeN5DyshT53t7q4VwCzuCXFT
3zRHVRHQnO6LHgZx1FuKfwtkhfSXDyYU2fQYw2Hcb9krYU/alViVZdE0rENXCClq
xO7AQk5MJcGg6cfE5wWAKU1ATjpK4CN+RTn8v8ODLoI2SW3pfsnXxm93O+pp9HN4
+O+1PQtNUWhCfh+g6BN2mYo2OEZ8qGSxDlMZej4YOdVkW8PHmFZTK0w9iJKqM5o1
V6g5gZlqSoRhICK09tpc
-----END CERTIFICATE-----`
	configDumpPath = "testdata/envoyconfigdumpsds.json"
)

var (
	singleAgentResponse = map[string]sds.Debug{
		"test-agent-1": {
			Clients: []sds.ClientDebug{
				{
					ConnectionID: "connection-id",
					ProxyID:      "proxy-id",
					ResourceName: "resource-name",
				},
			},
		},
	}

	validCertResponse = map[string]sds.Debug{
		"test-agent-1": {
			Clients: []sds.ClientDebug{
				{
					ConnectionID: "connection-id",
					ProxyID:      "proxy-id",
					ResourceName: "resource-name-a",
					RootCert:     exampleCACert,
				},
			},
		},
		"test-agent-2": {
			Clients: []sds.ClientDebug{
				{
					ConnectionID: "connection-id",
					ProxyID:      "proxy-id",
					ResourceName: "resource-name-b",
					RootCert:     exampleCACert,
				},
			},
		},
	}
)

func TestGetNodeAgentSecrets(t *testing.T) {
	tests := []struct {
		name           string
		agentResponses map[string]sds.Debug
		connNameFilter func(string) bool
		expectedOutput []SecretItem
		wantErr        bool
	}{
		{
			name:           "empty sds debug response parses into empty secret item list",
			agentResponses: map[string]sds.Debug{},
			connNameFilter: filterNone,
			expectedOutput: []SecretItem{},
			wantErr:        false,
		},
		{
			name:           "SDS debug response from single node with blank data parses",
			agentResponses: singleAgentResponse,
			connNameFilter: filterNone,
			expectedOutput: []SecretItem{
				{
					Name:        "resource-name",
					Source:      "test-agent-1",
					Destination: "proxy-id",
					SecretMeta: SecretMeta{
						Valid: false,
					},
				},
			},
			wantErr: false,
		},
		{
			name:           "SDS debug response with valid certificate parses field",
			agentResponses: validCertResponse,
			connNameFilter: filterNone,
			expectedOutput: []SecretItem{
				{
					Name:        "resource-name-a",
					Source:      "test-agent-1",
					Destination: "proxy-id",
					Data:        exampleCACert,
					SecretMeta: SecretMeta{
						Valid:        true,
						SerialNumber: "146151220826062972802318521739440941340",
						NotAfter:     "2024-05-14T23:11:26Z",
						NotBefore:    "2019-05-16T22:11:26Z",
						Type:         "CA",
					},
				},
				{
					Name:        "resource-name-b",
					Source:      "test-agent-2",
					Destination: "proxy-id",
					Data:        exampleCACert,
					SecretMeta: SecretMeta{
						Valid:        true,
						SerialNumber: "146151220826062972802318521739440941340",
						NotAfter:     "2024-05-14T23:11:26Z",
						NotBefore:    "2019-05-16T22:11:26Z",
						Type:         "CA",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, err := GetNodeAgentSecrets(tc.agentResponses, tc.connNameFilter)
			if tc.wantErr != (err != nil) {
				t.Errorf("expected error: %t, but got: %t, error: %v", tc.wantErr, err != nil, err)
			}
			for _, secret := range tc.expectedOutput {
				if !secretItemPresent(secret, output) {
					t.Errorf("couldn't find expected secret item: %v", secret)
				}
			}
			if len(tc.expectedOutput) != len(output) {
				t.Errorf("expected secret count: %d, but got: %d", len(tc.expectedOutput), len(output))
			}
		})
	}
}

func secretItemPresent(item SecretItem, l []SecretItem) bool {
	for _, secretItem := range l {
		if reflect.DeepEqual(item, secretItem) {
			return true
		}
	}
	return false
}

func filterNone(n string) bool { return true }

func TestGetEnvoyActiveSecrets(t *testing.T) {
	tests := []struct {
		name            string
		inputFile       string
		wantErr         bool
		expectedSecrets int
	}{
		{
			name:            "normal config dump should parse into a secret",
			inputFile:       configDumpPath,
			expectedSecrets: 4,
			wantErr:         false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rawDump, _ := ioutil.ReadFile(tc.inputFile)
			dump := &configdump.Wrapper{}
			json.Unmarshal(rawDump, dump)

			output, err := GetEnvoySecrets(dump)
			if tc.wantErr != (err != nil) {
				t.Errorf("expected error: %t, but got: %t, error: %v", tc.wantErr, err != nil, err)
			}
			if len(output) != tc.expectedSecrets {
				t.Errorf("expected %d secrets, but got: %d", tc.expectedSecrets, len(output))
			}
		})
	}
}
