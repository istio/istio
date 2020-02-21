// Copyright 2020 Istio Authors
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

package sidecartls

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"text/template"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	SidecarConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
  namespace:  {{.AppNamespace}}
spec:
  ingress:
    - port:
        number: 9080
        protocol: HTTPS
        name: custom-http
      inboundTls:
        mode: SIMPLE
        serverCertificate: /custom/cert/servercert.pem
        privateKey: /custom/certs/privatekey.pem
      defaultEndpoint: unix:///var/run/someuds.sock
  egress:
    - hosts:
      - "*/*"
`
	AppConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - httpbin.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  endpoints:
  - address: 1.1.1.1
    labels:
      app: httpbin
`
	StrictMtls = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "default"
  namespace: {{.AppNamespace}}
spec:
  mtls:
    mode: STRICT
`
	DisableMTLS = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "default"
  namespace: {{.AppNamespace}}
spec:
  mtls:
    mode: DISABLE
`
)

type Config struct {
	AppNamespace string
}

func setupTest(t *testing.T, ctx resource.Context) (pilot.Instance, *model.Proxy, func()) {
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

	appNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})

	config := Config{
		AppNamespace: appNamespace.Name(),
	}

	// Apply all configs
	createConfig(t, g, config, SidecarConfig, appNamespace)
	createConfig(t, g, config, AppConfig, appNamespace)
	createConfig(t, g, config, StrictMtls, appNamespace)

	nodeID := &model.Proxy{
		ClusterID:   "integration-test",
		ID:          fmt.Sprintf("httpbin.%s", appNamespace.Name()),
		DNSDomain:   appNamespace.Name() + ".cluster.local",
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		Metadata: &model.NodeMetadata{
			Namespace: appNamespace.Name(),
		},
		ConfigNamespace: appNamespace.Name(),
	}
	return p, nodeID, func() {
		// allow disable mtls later
		deleteConfig(t, g, config, StrictMtls, appNamespace)
		createConfig(t, g, config, DisableMTLS, appNamespace)
	}
}

func createConfig(t *testing.T, g galley.Instance, config Config, yaml string, namespace namespace.Instance) {
	tmpl, err := template.New("Config").Parse(yaml)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.ApplyConfig(namespace, buf.String()); err != nil {
		t.Fatalf("failed to apply config: %v. Config: %v", err, buf.String())
	}
}

func deleteConfig(t *testing.T, g galley.Instance, config Config, yaml string, namespace namespace.Instance) {
	tmpl, err := template.New("Config").Parse(yaml)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := g.DeleteConfig(namespace, buf.String()); err != nil {
		t.Fatalf("failed to delete config: %v. Config: %v", err, buf.String())
	}
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("sidecar_custom_tls_test", m).
		RequireEnvironment(environment.Native).
		Run()
}

func TestSidecarCustomTLS(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		p, nodeID, disableMTLSFunc := setupTest(t, ctx)
		var nodeMetadata = &structpb.Struct{Fields: map[string]*structpb.Value{
			"ISTIO_VERSION": {Kind: &structpb.Value_StringValue{StringValue: "1.5"}},
			"NAMESPACE":     {Kind: &structpb.Value_StringValue{StringValue: nodeID.Metadata.Namespace}},
		}}
		listenerReq := &xdsapi.DiscoveryRequest{
			Node: &xdscore.Node{
				Id:       nodeID.ServiceNode(),
				Metadata: nodeMetadata,
			},
			TypeUrl: v2.ListenerType,
		}

		if err := p.StartDiscovery(listenerReq); err != nil {
			t.Fatal(err)
		}
		if err := p.WatchDiscovery(5*time.Second, checkInboundFilterChainMTLS); err != nil {
			t.Error(err)
		}
		// apply new PeerAuthentication to disable mtls
		disableMTLSFunc()
		if err := p.WatchDiscovery(5*time.Second, checkCustomInboundFilterChainTLS); err != nil {
			t.Error(err)
		}
	})
}

// Check inbound filter chain tls context
func checkInboundFilterChainMTLS(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expected := map[string]struct{}{
		"1.1.1.1_9080":    {},
		"0.0.0.0_80":      {},
		"virtualInbound":  {},
		"virtualOutbound": {},
	}

	expectedTLSContext := &auth.DownstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				{
					CertificateChain: &xdscore.DataSource{
						Specifier: &xdscore.DataSource_Filename{
							Filename: "/etc/certs/cert-chain.pem",
						},
					},
					PrivateKey: &xdscore.DataSource{
						Specifier: &xdscore.DataSource_Filename{
							Filename: "/etc/certs/key.pem",
						},
					},
				},
			},
			ValidationContextType: &auth.CommonTlsContext_ValidationContext{
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &xdscore.DataSource{
						Specifier: &xdscore.DataSource_Filename{
							Filename: "/etc/certs/root-cert.pem",
						},
					},
				},
			},
			AlpnProtocols: util.ALPNDownstream,
		},
		RequireClientCertificate: &wrappers.BoolValue{
			Value: true,
		},
	}

	got := map[string]struct{}{}
	for _, res := range resp.Resources {
		c := &xdsapi.Listener{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		got[c.Name] = struct{}{}
		if c.Name == "1.1.1.1_9080" {
			tlscontext := &auth.DownstreamTlsContext{}
			transportSocket := c.FilterChains[0].TransportSocket.GetTypedConfig()
			ptypes.UnmarshalAny(transportSocket, tlscontext)
			if !reflect.DeepEqual(expectedTLSContext, tlscontext) {
				return false, fmt.Errorf("expected tls context: %+v, got %+v\n diff: %s", expectedTLSContext, tlscontext, cmp.Diff(expectedTLSContext, tlscontext))
			}
		}

	}
	if !reflect.DeepEqual(expected, got) {
		return false, nil
	}
	return true, nil
}

// Check inbound filter chain tls context with custom ingress tls specified
func checkCustomInboundFilterChainTLS(resp *xdsapi.DiscoveryResponse) (success bool, e error) {
	expectedTLSContext := &auth.DownstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				{
					CertificateChain: &xdscore.DataSource{
						Specifier: &xdscore.DataSource_Filename{
							Filename: "/custom/cert/servercert.pem",
						},
					},
					PrivateKey: &xdscore.DataSource{
						Specifier: &xdscore.DataSource_Filename{
							Filename: "/custom/certs/privatekey.pem",
						},
					},
				},
			},
			AlpnProtocols: util.ALPNHttp,
		},
		RequireClientCertificate: &wrappers.BoolValue{
			Value: false,
		},
	}
	for _, res := range resp.Resources {
		c := &xdsapi.Listener{}
		if err := proto.Unmarshal(res.Value, c); err != nil {
			return false, err
		}
		if c.Name == "1.1.1.1_9080" {
			tlscontext := &auth.DownstreamTlsContext{}
			transportSocket := c.FilterChains[0].TransportSocket.GetTypedConfig()
			ptypes.UnmarshalAny(transportSocket, tlscontext)
			if !reflect.DeepEqual(expectedTLSContext, tlscontext) {
				return false, fmt.Errorf("expected tls context: %+v, got %+v\n diff: %s",
					expectedTLSContext, tlscontext, cmp.Diff(expectedTLSContext, tlscontext))
			}
			return true, nil
		}
	}
	// not matched
	return false, nil
}
