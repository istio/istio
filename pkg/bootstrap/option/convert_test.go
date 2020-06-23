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
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap/auth"
)

func TestTlsContextConvert(t *testing.T) {
	tests := []struct {
		desc         string
		tls          *networkingAPI.ClientTLSSettings
		sni          string
		meta         *model.BootstrapNodeMetadata
		expectTLSCtx *auth.UpstreamTLSContext
	}{
		{
			desc:         "no-tls",
			tls:          &networkingAPI.ClientTLSSettings{},
			sni:          "",
			meta:         &model.BootstrapNodeMetadata{},
			expectTLSCtx: nil,
		},
		{
			desc: "tls-simple-no-cert",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_SIMPLE,
			},
			sni:  "",
			meta: &model.BootstrapNodeMetadata{},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					ValidationContext: nil,
					AlpnProtocols:     util.ALPNH2Only,
				},
			},
		},
		{
			desc: "tls-simple-cert-cli",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:           networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "foo.pem",
				Sni:            "foo",
			},
			sni:  "",
			meta: &model.BootstrapNodeMetadata{},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &auth.DataSource{
							Filename: "foo.pem",
						},
					},
					AlpnProtocols: []string{"h2"},
				},
				Sni: "foo",
			},
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
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &auth.DataSource{
							Filename: "/foo/bar/baz.pem",
						},
					},
					AlpnProtocols: []string{"h2"},
				},
				Sni: "foo",
			},
		},
		{
			desc: "tls-cli-mutual-missing-certs",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_MUTUAL,
			},
			expectTLSCtx: nil,
		},
		{
			desc: "tls-cli-mutual",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:              networkingAPI.ClientTLSSettings_MUTUAL,
				ClientCertificate: "foo",
				PrivateKey:        "im-private-foo",
				Sni:               "bar",
			},
			sni:  "",
			meta: &model.BootstrapNodeMetadata{},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					TLSCertificates: []*auth.TLSCertificate{
						{
							CertificateChain: &auth.DataSource{
								Filename: "foo",
							},
							PrivateKey: &auth.DataSource{
								Filename: "im-private-foo",
							},
						},
					},
					AlpnProtocols: []string{"h2"},
				},
				Sni: "bar",
			},
		},
		{
			desc: "tls-istio-mutual-no-certs",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
			},
			sni:  "i-should-be-sni",
			meta: &model.BootstrapNodeMetadata{},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					TLSCertificates: []*auth.TLSCertificate{
						{
							CertificateChain: &auth.DataSource{
								Filename: "/etc/certs/root-cert.pem",
							},
							PrivateKey: &auth.DataSource{
								Filename: "/etc/certs/key.pem",
							},
						},
					},
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &auth.DataSource{
							Filename: "/etc/certs/cert-chain.pem",
						},
					},
					AlpnProtocols: []string{"istio", "h2"},
				},
				Sni: "i-should-be-sni",
			},
		},
		{
			desc: "tls-istio-mutual-provide-certs",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:              networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: "foo.pem",
				PrivateKey:        "bar.pem",
			},
			sni:  "i-should-be-sni",
			meta: &model.BootstrapNodeMetadata{},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					TLSCertificates: []*auth.TLSCertificate{
						{
							CertificateChain: &auth.DataSource{
								Filename: "foo.pem",
							},
							PrivateKey: &auth.DataSource{
								Filename: "bar.pem",
							},
						},
					},
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &auth.DataSource{
							Filename: "/etc/certs/cert-chain.pem",
						},
					},
					AlpnProtocols: []string{"istio", "h2"},
				},
				Sni: "i-should-be-sni",
			},
		},
		{
			desc: "tls-istio-mutual-meta-certs",
			tls: &networkingAPI.ClientTLSSettings{
				Mode:              networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: "foo.pem",
				PrivateKey:        "bar.pem",
			},
			sni: "i-should-be-sni",
			meta: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					TLSClientCertChain: "better-foo.pem",
					TLSClientKey:       "better-bar.pem",
				},
			},
			expectTLSCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					TLSCertificates: []*auth.TLSCertificate{
						{
							CertificateChain: &auth.DataSource{
								Filename: "better-foo.pem",
							},
							PrivateKey: &auth.DataSource{
								Filename: "better-bar.pem",
							},
						},
					},
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &auth.DataSource{
							Filename: "/etc/certs/cert-chain.pem",
						},
					},
					AlpnProtocols: []string{"istio", "h2"},
				},
				Sni: "i-should-be-sni",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := tlsContextConvert(tt.tls, tt.sni, tt.meta); !reflect.DeepEqual(tt.expectTLSCtx, got) {
				t.Errorf("%s: expected TLS ctx %v got %v", tt.desc, tt.expectTLSCtx, got)
			}
		})
	}
}
