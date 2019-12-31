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

// Package basic contains an example test suite for showcase purposes.
package security

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/tests/integration/security/util"
)

func verifyListenerForTLSServerDNSCert(listener *xdsapi.Listener, t *testing.T) error {
	t.Helper()
	if listener == nil {
		return errors.New("no such listener")
	}
	if len(listener.ListenerFilters) == 0 {
		return errors.New("no listener filter")
	}
	// tls_inspector filter should exist
	inspector := false
	for _, lf := range listener.ListenerFilters {
		if lf.Name == xdsutil.TlsInspector {
			inspector = true
			break
		}
	}
	if !inspector {
		return errors.New("no tls inspector")
	}
	// 3 filter chains are expected
	if l := len(listener.FilterChains); l != 3 {
		return fmt.Errorf("expect exactly 3 filter chains, actually %d", l)
	}

	// First filter chain should have filter chain with alpn matches for "istio"
	// and "istio-http/1.0", "istio-http/1.1" and "istio-h2" as added by istio.alpn filter
	mtlsChain := listener.FilterChains[0]
	if !reflect.DeepEqual(mtlsChain.FilterChainMatch.GetTransportProtocol(), "tls") {
		return errors.New("mtlsChain transport protocol is not tls")
	}
	if !reflect.DeepEqual(mtlsChain.FilterChainMatch.GetApplicationProtocols(), []string{"istio", "istio-http/1.0", "istio-http/1.1", "istio-h2"}) {
		return errors.New("mtlsChain alpn is not set to istio, istio-http/1.0, istio-http/1.1 and istio-h2")
	}
	if mtlsChain.TransportSocket == nil {
		return errors.New("mtlsChain transport socket is empty")
	}

	// Second default filter chain should have empty filter chain match and no tls context.
	defaultChain := listener.FilterChains[1]
	if l := len(defaultChain.FilterChainMatch.GetApplicationProtocols()); l != 0 {
		return fmt.Errorf("expected empty alpn, actually %v", defaultChain.FilterChainMatch.GetApplicationProtocols())
	}
	if defaultChain.TlsContext != nil {
		return errors.New("defaultChain has non empty tls context")
	}

	// Third filter chain should have filter chain match with transport protocol as "tls"
	transportProtocolChain := listener.FilterChains[2]
	if !reflect.DeepEqual(transportProtocolChain.FilterChainMatch.GetTransportProtocol(), "tls") {
		return errors.New("transportProtocolChain transport protocol is not tls")
	}
	if transportProtocolChain.TransportSocket == nil {
		return errors.New("transportProtocolChain transport socket is empty")
	}

	// TLS certificate and key paths should be set to the custom dir set through metadata value of TLS_SERVER_DNS_CERT
	tlsContext := &envoy_api_v2_auth.DownstreamTlsContext{}
	err := ptypes.UnmarshalAny(transportProtocolChain.GetTransportSocket().GetTypedConfig(), tlsContext)
	if err == nil {
		if !reflect.DeepEqual(tlsContext.GetCommonTlsContext().GetTlsCertificates()[0].GetCertificateChain().GetFilename(), "/etc/certs/custom/cert-chain.pem") {
			return errors.New("tls cert file name is not /etc/certs/custom/cert-chain.pem")
		}
		if !reflect.DeepEqual(tlsContext.GetCommonTlsContext().GetTlsCertificates()[0].GetPrivateKey().GetFilename(), "/etc/certs/custom/key.pem") {
			return errors.New("tls key file name is not /etc/certs/custom/key.pem")
		}
		if !reflect.DeepEqual(tlsContext.GetCommonTlsContext().GetValidationContext().GetTrustedCa().GetFilename(), "/etc/certs/custom/root-cert.pem") {
			return errors.New("tls ca file name is not /etc/certs/custom/root-cert.pem")
		}
	} else {
		return errors.New("unable to unmarshal tls context")
	}
	return nil
}

// TestTLSServerDNSCertWithAuthnPermissive checks when authentication policy is permissive,
// and metadata value of TLS_SERVER_DNS_CERT is passed to Pilot, it generates expected
// listener configuration with additional filter chain with match as transport_protocol: "tls"
func TestTLSServerDNSCertWithAuthnPermissive(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "permmtls",
				Inject: true,
			})
			policy := `
apiVersion: authentication.istio.io/v1alpha1
kind: Policy
metadata:
  name: default
spec:
  peers:
    - mtls:
        mode: PERMISSIVE
`
			g.ApplyConfigOrFail(t, ns, policy)

			var b echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&b, util.EchoConfig("b", ns, false, nil, g, p)).
				BuildOrFail(t)

			t.Logf("echo boot warmed")
			nodeID := b.WorkloadsOrFail(t)[0].Sidecar().NodeID()

			// Set custom dir path for TLS_SERVER_DNS_CERT
			echoNodeMetadata := &model.NodeMetadata{
				TLSServerDNSCert: "/etc/certs/custom",
			}

			newDiscoveryRequest := &xdsapi.DiscoveryRequest{
				Node: &xdscore.Node{
					Id:       nodeID,
					Metadata: echoNodeMetadata.ToStruct(),
				},
				TypeUrl: string(pilot.Listener),
			}

			// Start the xDS stream containing the listeners for this node
			p.StartDiscoveryOrFail(t, newDiscoveryRequest)

			p.WatchDiscoveryOrFail(t, time.Second*60,
				func(resp *xdsapi.DiscoveryResponse) (b bool, e error) {
					var errs []error
					for _, r := range resp.Resources {
						foo := &xdsapi.Listener{}
						err := ptypes.UnmarshalAny(r, foo)
						if err != nil {
							errs = append(errs, err)
							continue
						}
						// Only test inbound listeners
						if foo.GetTrafficDirection() == xdscore.TrafficDirection_OUTBOUND {
							continue
						}

						err = verifyListenerForTLSServerDNSCert(foo, t)
						if err != nil {
							errs = append(errs, fmt.Errorf("listener %s has error %s. details: %s", foo.Name, err, foo.String()))
							continue
						}

						return true, nil
					}
					return false, fmt.Errorf("no inbound listener passes the validation. Errors: %v", errs)
				})
		})
}
