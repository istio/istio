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

	"istio.io/istio/tests/integration/security/util"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

func verifyListener(listener *xdsapi.Listener, t *testing.T) error {
	t.Helper()
	if listener == nil {
		return errors.New("no such listener")
	}
	if len(listener.ListenerFilters) == 0 {
		return errors.New("no listener filter")
	}
	// We expect tls_inspector filter exist.
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
	// Check filter chain match.
	if l := len(listener.FilterChains); l != 2 {
		return fmt.Errorf("expect exactly 2 filter chains, actually %d", l)
	}
	mtlsChain := listener.FilterChains[0]
	if !reflect.DeepEqual(mtlsChain.FilterChainMatch.ApplicationProtocols, []string{"istio-peer-exchange", "istio"}) {
		return errors.New("alpn is not istio")
	}
	if mtlsChain.TransportSocket == nil {
		return errors.New("transport socket is empty")
	}
	// Second default filter chain should have empty filter chain match and no tls context.
	defaultChain := listener.FilterChains[1]
	if l := len(defaultChain.FilterChainMatch.ApplicationProtocols); l != 0 {
		return fmt.Errorf("expect empty alpn, actually %v", defaultChain.FilterChainMatch.ApplicationProtocols)
	}
	if defaultChain.TlsContext != nil {
		return errors.New("non empty tls context")
	}
	return nil
}

// TestAuthnPermissive checks when authentication policy is permissive, Pilot generates expected
// listener configuration.
func TestAuthnPermissive(t *testing.T) {
	framework.NewTest(t).
		// TODO(incfly): make test able to run both on k8s and native when galley is ready.
		RequiresEnvironment(environment.Native).
		Run(func(ctx framework.TestContext) {

			env := ctx.Environment().(*native.Environment)
			ns := namespace.ClaimOrFail(t, ctx, env.SystemNamespace)

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

			var a echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, util.EchoConfig("a", ns, false, nil, g, p)).
				BuildOrFail(t)
			t.Logf("echo boot warmed")
			nodeID := a.WorkloadsOrFail(t)[0].Sidecar().NodeID()
			req := pilot.NewDiscoveryRequest(nodeID, pilot.Listener)
			err := p.StartDiscovery(req)
			if err != nil {
				t.Fatalf("failed to call discovery %v", err)
			}
			p.WatchDiscoveryOrFail(t, time.Second*30,
				func(resp *xdsapi.DiscoveryResponse) (b bool, e error) {
					var errs []error
					for _, r := range resp.Resources {
						foo := &xdsapi.Listener{}
						err := ptypes.UnmarshalAny(r, foo)
						if err != nil {
							errs = append(errs, err)
							continue
						}
						//TODO(silentdai): use listener meta data to validate all the listeners: in/out, virtual/nonvirtual
						err = verifyListener(foo, t)
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

// TestAuthentictionPermissiveE2E these cases are covered end to end
// app A to app B using plaintext (mTLS),
// app A to app B using HTTPS (mTLS),
// app A to app B using plaintext (legacy),
// app A to app B using HTTPS (legacy).
// explained: app-to-app-protocol(sidecar-to-sidecar-protocol). "legacy" means
// no client sidecar, unable to send "istio" alpn indicator.
// TODO(incfly): implement this
// func TestAuthentictionPermissiveE2E(t *testing.T) {
// Steps:
// Configure authn policy.
// Wait for config propagation.
// Send HTTP requests between apps.
// }
