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
	"reflect"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	lis "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	proto "github.com/gogo/protobuf/types"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	appst "istio.io/istio/pkg/test/framework/runtime/components/apps"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// To opt-in to the test framework, implement a TestMain, and call test.Run.
func TestMain(m *testing.M) {
	framework.Run("permissive_test", m)
}

func veriyListener(listener *xdsapi.Listener, t *testing.T) bool {
	t.Helper()
	if listener == nil {
		return false
	}
	if len(listener.ListenerFilters) == 0 {
		return false
	}
	inspector := false
	for _, lf := range listener.ListenerFilters {
		if lf.Name == authn.EnvoyTLSInspectorFilterName {
			// fmt.Printf("listner is %+v\n", *listener)
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
	defaultChain := listener.FilterChains[1]
	if !reflect.DeepEqual(defaultChain.FilterChainMatch, &lis.FilterChainMatch{}) {
		return false
	}
	if defaultChain.TlsContext != nil {
		return false
	}
	return true
}

func TestPermissive(t *testing.T) {
	ctx := framework.GetContext(t)
	// TODO(incfly): make test able to run both on k8s and native when galley is ready.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment, &ids.Apps)
	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	pilot := components.GetPilot(ctx, t)
	req := appst.ConstructDiscoveryRequest(a)
	resp, err := pilot.CallDiscovery(req)
	if err != nil {
		t.Errorf("failed to call discovery %v", err)
	}
	for _, r := range resp.Resources {
		foo := &xdsapi.Listener{}
		if err := proto.UnmarshalAny(&r, foo); err != nil {
			t.Errorf("failed to unmarshal %v", err)
		}
		if veriyListener(foo, t) {
			return
		}
	}
	t.Errorf("failed to find any listeners having multiplexing filter chain")
}
