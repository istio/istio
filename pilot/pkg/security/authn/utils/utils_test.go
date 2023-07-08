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

package utils

import (
	"testing"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	model "istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
)

func TestGetMinTLSVersion(t *testing.T) {
	tests := []struct {
		name              string
		minTLSVer         meshconfig.MeshConfig_TLSConfig_TLSProtocol
		expectedMinTLSVer tls.TlsParameters_TlsProtocol
	}{
		{
			name:              "Default TLS versions",
			expectedMinTLSVer: tls.TlsParameters_TLSv1_2,
		},
		{
			name:              "Configure minimum TLS version 1.2",
			minTLSVer:         meshconfig.MeshConfig_TLSConfig_TLSV1_2,
			expectedMinTLSVer: tls.TlsParameters_TLSv1_2,
		},
		{
			name:              "Configure minimum TLS version 1.3",
			minTLSVer:         meshconfig.MeshConfig_TLSConfig_TLSV1_3,
			expectedMinTLSVer: tls.TlsParameters_TLSv1_3,
		},
		{
			name:              "Configure minimum TLS version to be auto",
			minTLSVer:         meshconfig.MeshConfig_TLSConfig_TLS_AUTO,
			expectedMinTLSVer: tls.TlsParameters_TLSv1_2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minVersion := GetMinTLSVersion(tt.minTLSVer)
			if minVersion != tt.expectedMinTLSVer {
				t.Errorf("unexpected result: expected min ver %v got %v",
					tt.expectedMinTLSVer, minVersion)
			}
		})
	}
}

func TestGetMTLSCipherSuites(t *testing.T) {
	tests := []struct {
		name                     string
		mesh                     meshconfig.MeshConfig
		expectedMTLSCipherSuites []string
	}{
		{
			name:                     "Default MTLS supported Ciphers",
			expectedMTLSCipherSuites: SupportedCiphers,
		},
		{
			name: "Configure 1 MTLS cipher suite",
			mesh: meshconfig.MeshConfig{
				MeshMTLS: &meshconfig.MeshConfig_TLSConfig{
					CipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384"},
				},
			},
			expectedMTLSCipherSuites: []string{"ECDHE-RSA-AES256-GCM-SHA384"},
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			testNode := &model.Proxy{
				Labels: map[string]string{
					"app": "foo",
				},
				Metadata: &model.NodeMetadata{},
			}

			got := BuildInboundTLS(model.MTLSStrict, testNode, networking.ListenerProtocolTCP, []string{}, tls.TlsParameters_TLSv1_2, &tt.mesh)
			if diff := cmp.Diff(tt.expectedMTLSCipherSuites, got.CommonTlsContext.TlsParams.CipherSuites, protocmp.Transform()); diff != "" {
				t.Errorf("unexpected cipher suites: %v", diff)
			}
		})
	}
}
