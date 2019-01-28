//  Copyright 2018 Istio Authors
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

// Package basic contains an example test suite for showcase purposes.
package security

import (
	"os"
	"reflect"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	lis "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	proto "github.com/gogo/protobuf/types"

	authnplugin "istio.io/istio/pilot/pkg/networking/plugin/authn"
	appst "istio.io/istio/pkg/test/framework/runtime/components/apps"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

const (
	timeout       = 10 * time.Second
	retryInterval = 500 * time.Millisecond
)

// To opt-in to the test framework, implement a TestMain, and call test.Run.
func TestMain(m *testing.M) {
	rt, _ := framework.Run("authn_permissive_test", m)
	os.Exit(rt)
}

func verifyListener(listener *xdsapi.Listener, t *testing.T) bool {
	t.Helper()
	if listener == nil {
		return false
	}
	if len(listener.ListenerFilters) == 0 {
		return false
	}
	// We expect tls_inspector filter exist.
	inspector := false
	for _, lf := range listener.ListenerFilters {
		if lf.Name == authnplugin.EnvoyTLSInspectorFilterName {
			inspector = true
			break
		}
	}
	if !inspector {
		return false
	}
	// Check filter chain match.
	if len(listener.FilterChains) != 2 {
		return false
	}
	mtlsChain := listener.FilterChains[0]
	if !reflect.DeepEqual(mtlsChain.FilterChainMatch.ApplicationProtocols, []string{"istio"}) {
		return false
	}
	if mtlsChain.TlsContext == nil {
		return false
	}
	// Second default filter chain should have empty filter chain match and no tls context.
	defaultChain := listener.FilterChains[1]
	if !reflect.DeepEqual(defaultChain.FilterChainMatch, &lis.FilterChainMatch{}) {
		return false
	}
	if defaultChain.TlsContext != nil {
		return false
	}
	return true
}

// TestAuthnPermissive checks when authentication policy is permissive, Pilot generates expected
// listener configuration.
func TestAuthnPermissive(t *testing.T) {
	config := `
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "default"
  namespace: "istio-system"
spec:
  peers:
    - mtls:
        mode: PERMISSIVE
`
	ctx := framework.GetContext(t)
	// TODO(incfly): make test able to run both on k8s and native when galley is ready.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment, &ids.Apps)
	galley := components.GetGalley(ctx, t)
	galley.ApplyConfig(config)
	galley.WaitForSnapshot("istio/authentication/v1alpha1/policies")
	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	pilot := components.GetPilot(ctx, t)
	endTime := time.Now().Add(timeout)
	for {
		req := appst.ConstructDiscoveryRequest(a, "type.googleapis.com/envoy.api.v2.Listener")
		resp, err := pilot.CallDiscovery(req)
		if err != nil {
			t.Errorf("failed to call discovery %v", err)
		}
		for _, r := range resp.Resources {
			foo := &xdsapi.Listener{}
			err := proto.UnmarshalAny(&r, foo)
			result := verifyListener(foo, t)
			if err == nil && result {
				return
			}
		}
		if time.Now().After(endTime) {
			t.Fatalf("failed to find any listeners having multiplexing filter chain")
		}
		time.Sleep(retryInterval)
	}
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
