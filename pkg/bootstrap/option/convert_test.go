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

package option

import (
	"reflect"
	"testing"

	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

// nolint: lll
func TestTlsContextConvert(t *testing.T) {
	tests := []struct {
		desc         string
		tls          *networkingAPI.ClientTLSSettings
		sni          string
		meta         *model.BootstrapNodeMetadata
		expectTLSCtx string
	}{
		{
			desc:         "no-tls",
			tls:          &networkingAPI.ClientTLSSettings{},
			sni:          "",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: "null",
		},
		{
			desc: "tls-simple-no-cert",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_SIMPLE,
			},
			sni:          "",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: "{\"common_tls_context\":{\"ValidationContextType\":{\"CombinedValidationContext\":{\"default_validation_context\":{}}},\"alpn_protocols\":[\"h2\"]}}",
		},
		{
			desc: "tls-simple-cert-cli",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:           networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "foo.pem",
				Sni:            "foo",
			},
			sni:          "",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: `{"common_tls_context":{"ValidationContextType":{"CombinedValidationContext":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"file-root:foo.pem","sds_config":{"ConfigSourceSpecifier":{"ApiConfigSource":{"api_type":2,"transport_api_version":2,"grpc_services":[{"TargetSpecifier":{"EnvoyGrpc":{"cluster_name":"sds-grpc"}}}]}},"resource_api_version":2}}}},"alpn_protocols":["h2"]},"sni":"foo"}`,
		},
		{
			desc: "tls-simple-cert-cli-meta",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:           networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "foo.pem",
				Sni:            "foo",
			},
			sni: "",
			meta: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					TLSClientRootCert: "/foo/bar/baz.pem",
				},
			},
			expectTLSCtx: `{"common_tls_context":{"ValidationContextType":{"CombinedValidationContext":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"file-root:/foo/bar/baz.pem","sds_config":{"ConfigSourceSpecifier":{"ApiConfigSource":{"api_type":2,"transport_api_version":2,"grpc_services":[{"TargetSpecifier":{"EnvoyGrpc":{"cluster_name":"sds-grpc"}}}]}},"resource_api_version":2}}}},"alpn_protocols":["h2"]},"sni":"foo"}`,
		},
		{
			desc: "tls-cli-mutual-missing-certs",
			meta: &model.BootstrapNodeMetadata{},
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_MUTUAL,
			},
			expectTLSCtx: `{"common_tls_context":{"ValidationContextType":{"CombinedValidationContext":{"default_validation_context":{}}},"alpn_protocols":["h2"]}}`,
		},
		{
			desc: "tls-cli-mutual",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:              networkingAPI.ClientTLSSettings_MUTUAL,
				ClientCertificate: "foo",
				PrivateKey:        "im-private-foo",
				Sni:               "bar",
			},
			sni:          "",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: `{"common_tls_context":{"tls_certificate_sds_secret_configs":[{"name":"file-cert:foo~im-private-foo","sds_config":{"ConfigSourceSpecifier":{"ApiConfigSource":{"api_type":2,"transport_api_version":2,"grpc_services":[{"TargetSpecifier":{"EnvoyGrpc":{"cluster_name":"sds-grpc"}}}]}},"resource_api_version":2}}],"ValidationContextType":{"CombinedValidationContext":{"default_validation_context":{}}},"alpn_protocols":["h2"]},"sni":"bar"}`,
		},
		{
			desc: "tls-istio-mutual-no-certs",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
			},
			sni:          "i-should-be-sni",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: `{"common_tls_context":{"tls_certificate_sds_secret_configs":[{"name":"default","sds_config":{"ConfigSourceSpecifier":{"ApiConfigSource":{"api_type":2,"transport_api_version":2,"grpc_services":[{"TargetSpecifier":{"EnvoyGrpc":{"cluster_name":"sds-grpc"}}}]}},"initial_fetch_timeout":{},"resource_api_version":2}}],"ValidationContextType":{"CombinedValidationContext":{"default_validation_context":{},"validation_context_sds_secret_config":{"name":"ROOTCA","sds_config":{"ConfigSourceSpecifier":{"ApiConfigSource":{"api_type":2,"transport_api_version":2,"grpc_services":[{"TargetSpecifier":{"EnvoyGrpc":{"cluster_name":"sds-grpc"}}}]}},"initial_fetch_timeout":{},"resource_api_version":2}}}},"alpn_protocols":["istio","h2"]},"sni":"i-should-be-sni"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := convertToJSON(tlsContextConvert(tt.tls, tt.sni, tt.meta)); !reflect.DeepEqual(tt.expectTLSCtx, got) {
				t.Errorf("%s: expected TLS ctx \n%v got \n%v", tt.desc, tt.expectTLSCtx, got)
			}
		})
	}
}
