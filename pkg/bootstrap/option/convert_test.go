package option

import (
	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap/auth"
	"reflect"
	"testing"
)

func TestTlsContextConvert(t *testing.T) {
	tests := []struct{
		desc string
		tls *networkingAPI.ClientTLSSettings
		sni string
		meta *model.NodeMetadata
		expectTlsCtx *auth.UpstreamTLSContext
	}{
		{
			desc: "no-tls",
			tls: &networkingAPI.ClientTLSSettings{},
			sni: "",
			meta: &model.NodeMetadata{},
			expectTlsCtx: nil,
		},
		{
			desc: "tls-simple-no-cert",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_SIMPLE,
			},
			sni: "",
			meta: &model.NodeMetadata{},
			expectTlsCtx: &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					ValidationContext: nil,
					AlpnProtocols: util.ALPNH2Only,
				},
			},
		},
		{
			desc: "tls-simple-cert-cli",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "foo.pem",
				Sni: "foo",
			},
			sni: "",
			meta: &model.NodeMetadata{},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
				Mode: networkingAPI.ClientTLSSettings_SIMPLE,
				CaCertificates: "foo.pem",
				Sni: "foo",
			},
			sni: "",
			meta: &model.NodeMetadata{
				TLSClientRootCert: "/foo/bar/baz.pem",
			},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
			expectTlsCtx: nil,
		},
		{
			desc: "tls-cli-mutual",
			tls: &networkingAPI.ClientTLSSettings{
				Mode: networkingAPI.ClientTLSSettings_MUTUAL,
				ClientCertificate: "foo",
				PrivateKey: "im-private-foo",
				Sni: "bar",
			},
			sni: "",
			meta: &model.NodeMetadata{},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
			sni: "i-should-be-sni",
			meta: &model.NodeMetadata{},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: "foo.pem",
				PrivateKey: "bar.pem",
			},
			sni: "i-should-be-sni",
			meta: &model.NodeMetadata{},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
				Mode: networkingAPI.ClientTLSSettings_ISTIO_MUTUAL,
				ClientCertificate: "foo.pem",
				PrivateKey: "bar.pem",
			},
			sni: "i-should-be-sni",
			meta: &model.NodeMetadata{
				TLSClientCertChain: "better-foo.pem",
				TLSClientKey: "better-bar.pem",
			},
			expectTlsCtx: &auth.UpstreamTLSContext{
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
			if got := tlsContextConvert(tt.tls, tt.sni, tt.meta); !reflect.DeepEqual(tt.expectTlsCtx, got) {
				t.Errorf("%s: expected TLS ctx %v got %v", tt.desc, tt.expectTlsCtx, got)
			}
		})
	}
}