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

package core

import (
	"fmt"
	"reflect"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	proxyprotocol "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/constants"
)

func TestApplyUpstreamProxyProtocol(t *testing.T) {
	istioMutualTLSSettings := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}
	mutualTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    constants.DefaultRootCert,
		ClientCertificate: constants.DefaultCertChain,
		PrivateKey:        constants.DefaultKey,
		SubjectAltNames:   []string{"custom.foo.com"},
		Sni:               "custom.foo.com",
	}
	simpleTLSSettingsWithCerts := &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_SIMPLE,
		CaCertificates:  constants.DefaultRootCert,
		SubjectAltNames: []string{"custom.foo.com"},
		Sni:             "custom.foo.com",
	}

	tests := []struct {
		name                       string
		mtlsCtx                    mtlsContextType
		discoveryType              cluster.Cluster_DiscoveryType
		tls                        *networking.ClientTLSSettings
		proxyProtocolSettings      *networking.TrafficPolicy_ProxyProtocol
		expectTransportSocket      bool
		expectTransportSocketMatch bool

		validateTLSContext func(t *testing.T, ctx *tls.UpstreamTlsContext)
	}{
		{
			name:          "user specified without tls",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           nil,
			proxyProtocolSettings: &networking.TrafficPolicy_ProxyProtocol{
				Version: networking.TrafficPolicy_ProxyProtocol_V2,
			},
			expectTransportSocket:      false,
			expectTransportSocketMatch: false,
		},
		{
			name:          "user specified with istio_mutual tls",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           istioMutualTLSSettings,
			proxyProtocolSettings: &networking.TrafficPolicy_ProxyProtocol{
				Version: networking.TrafficPolicy_ProxyProtocol_V2,
			},
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
		{
			name:          "user specified simple tls",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           simpleTLSSettingsWithCerts,
			proxyProtocolSettings: &networking.TrafficPolicy_ProxyProtocol{
				Version: networking.TrafficPolicy_ProxyProtocol_V2,
			},
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != simpleTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", simpleTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:          "user specified mutual tls",
			mtlsCtx:       userSupplied,
			discoveryType: cluster.Cluster_EDS,
			tls:           mutualTLSSettingsWithCerts,
			proxyProtocolSettings: &networking.TrafficPolicy_ProxyProtocol{
				Version: networking.TrafficPolicy_ProxyProtocol_V2,
			},
			expectTransportSocket:      true,
			expectTransportSocketMatch: false,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				rootName := "file-root:" + mutualTLSSettingsWithCerts.CaCertificates
				certName := fmt.Sprintf("file-cert:%s~%s", mutualTLSSettingsWithCerts.ClientCertificate, mutualTLSSettingsWithCerts.PrivateKey)
				if got := ctx.CommonTlsContext.GetCombinedValidationContext().GetValidationContextSdsSecretConfig().GetName(); rootName != got {
					t.Fatalf("expected root name %v got %v", rootName, got)
				}
				if got := ctx.CommonTlsContext.GetTlsCertificateSdsSecretConfigs()[0].GetName(); certName != got {
					t.Fatalf("expected cert name %v got %v", certName, got)
				}
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); got != nil {
					t.Fatalf("expected alpn list nil as not h2 or Istio_Mutual TLS Setting; got %v", got)
				}
				if got := ctx.GetSni(); got != mutualTLSSettingsWithCerts.Sni {
					t.Fatalf("expected TLSContext SNI %v; got %v", mutualTLSSettingsWithCerts.Sni, got)
				}
			},
		},
		{
			name:          "auto detect with tls",
			mtlsCtx:       autoDetected,
			discoveryType: cluster.Cluster_EDS,
			tls:           istioMutualTLSSettings,
			proxyProtocolSettings: &networking.TrafficPolicy_ProxyProtocol{
				Version: networking.TrafficPolicy_ProxyProtocol_V2,
			},
			expectTransportSocket:      false,
			expectTransportSocketMatch: true,
			validateTLSContext: func(t *testing.T, ctx *tls.UpstreamTlsContext) {
				if got := ctx.CommonTlsContext.GetAlpnProtocols(); !reflect.DeepEqual(got, util.ALPNInMeshWithMxc) {
					t.Fatalf("expected alpn list %v; got %v", util.ALPNInMeshWithMxc, got)
				}
			},
		},
	}

	proxy := &model.Proxy{
		Type:         model.SidecarProxy,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 5},
	}
	push := model.NewPushContext()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cb := NewClusterBuilder(proxy, &model.PushRequest{Push: push}, model.DisabledCache{})
			opts := &buildClusterOpts{
				mutable: newClusterWrapper(&cluster.Cluster{
					ClusterDiscoveryType: &cluster.Cluster_Type{Type: test.discoveryType},
				}),
				mesh: push.Mesh,
			}
			cb.applyUpstreamTLSSettings(opts, test.tls, test.mtlsCtx)
			// apply proxy protocol settings
			cb.applyUpstreamProxyProtocol(opts, test.proxyProtocolSettings)
			cluster := opts.mutable.cluster
			if test.expectTransportSocket && cluster.TransportSocket == nil ||
				!test.expectTransportSocket && cluster.TransportSocket != nil {
				t.Errorf("Expected TransportSocket %v", test.expectTransportSocket)
			}
			if test.expectTransportSocketMatch && cluster.TransportSocketMatches == nil ||
				!test.expectTransportSocketMatch && cluster.TransportSocketMatches != nil {
				t.Errorf("Expected TransportSocketMatch %v", test.expectTransportSocketMatch)
			}
			upstreamProxyProtocol := &proxyprotocol.ProxyProtocolUpstreamTransport{}
			if cluster.TransportSocket != nil {
				if got := cluster.TransportSocket.Name; got != "envoy.transport_sockets.upstream_proxy_protocol" {
					t.Errorf("Expected TransportSocket name %v, got %v", "envoy.transport_sockets.upstream_proxy_protocol", got)
				}
				if err := cluster.TransportSocket.GetTypedConfig().UnmarshalTo(upstreamProxyProtocol); err != nil {
					t.Fatal(err)
				}
				if upstreamProxyProtocol.Config.Version != core.ProxyProtocolConfig_Version(test.proxyProtocolSettings.Version) {
					t.Errorf("Expected proxy protocol version %v, got %v", test.proxyProtocolSettings.Version, upstreamProxyProtocol.Config.Version)
				}
				if test.validateTLSContext != nil {
					ctx := &tls.UpstreamTlsContext{}
					if err := upstreamProxyProtocol.TransportSocket.GetTypedConfig().UnmarshalTo(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}

			for i, match := range cluster.TransportSocketMatches {
				if err := match.TransportSocket.GetTypedConfig().UnmarshalTo(upstreamProxyProtocol); err != nil {
					t.Fatal(err)
				}
				if upstreamProxyProtocol.Config.Version != core.ProxyProtocolConfig_Version(test.proxyProtocolSettings.Version) {
					t.Errorf("Expected proxy protocol version %v, got %v", test.proxyProtocolSettings.Version, upstreamProxyProtocol.Config.Version)
				}
				if test.validateTLSContext != nil && i == 0 {
					ctx := &tls.UpstreamTlsContext{}
					if err := upstreamProxyProtocol.TransportSocket.GetTypedConfig().UnmarshalTo(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
