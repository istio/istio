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

package builder

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	header_mutation "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/header_mutation/v3"
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/wellknown"
)

func TestExtAuthzFilterStateIdentity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mesh           *meshconfig.MeshConfig
		useFilterState bool
		wantIdentity   bool
	}{
		{"waypoint-grpc", meshConfigGRPC, true, true},
		{"sidecar-grpc", meshConfigGRPC, false, false},
		{"waypoint-http", meshConfigHTTP, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := push(t, "http/custom-simple-http-in.yaml", tc.mesh)
			policies := p.AuthzPolicies.ListAuthorizationPolicies(model.PolicyMatcherForProxy(node(nil)))
			b := New(trustdomain.NewBundle("cluster.local", nil), p, policies, Option{
				IsCustomBuilder: true, UseFilterState: tc.useFilterState,
			})
			if b == nil {
				t.Fatal("CUSTOM policy did not produce a builder")
			}
			// Exercise repeated builds from the same resolved extension provider.
			first := b.BuildHTTP()
			for build := 0; build < 2; build++ {
				filters := b.BuildHTTP()
				if len(first) != len(filters) {
					t.Fatal("repeated build changed filter count")
				}
				for i := range first {
					if !proto.Equal(first[i], filters[i]) {
						t.Fatal("repeated build changed previously serialized filters")
					}
				}
				authzIndex := -1
				for i, f := range filters {
					if f.Name == wellknown.HTTPExternalAuthorization {
						authzIndex = i
					}
				}
				if authzIndex < 0 {
					t.Fatal("missing ext_authz filter")
				}
				foundIdentity := false
				for i, f := range filters {
					if f.GetTypedConfig().MessageIs(&header_mutation.HeaderMutation{}) {
						foundIdentity = true
						if i != authzIndex-1 {
							t.Fatal("peer identity must be overwritten immediately before ext_authz")
						}
						mutation := &header_mutation.HeaderMutation{}
						if err := f.GetTypedConfig().UnmarshalTo(mutation); err != nil {
							t.Fatal(err)
						}
						if err := mutation.ValidateAll(); err != nil {
							t.Fatal(err)
						}
						rules := mutation.GetMutations().GetRequestMutations()
						if len(rules) != 1 {
							t.Fatalf("got %d identity mutations, want 1", len(rules))
						}
						appendRule := rules[0].GetAppend()
						if appendRule.GetHeader().GetKey() != "x-istio-peer-principal" ||
							appendRule.GetHeader().GetValue() != "%FILTER_STATE(io.istio.peer_principal:PLAIN)%" {
							t.Fatalf("unexpected peer identity header: %v", appendRule)
						}
						if appendRule.GetAppendAction() != core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD ||
							!appendRule.GetKeepEmptyValue() {
							t.Fatal("peer identity must overwrite spoofed headers even when filter state is empty")
						}
					}
				}
				if foundIdentity != tc.wantIdentity {
					t.Fatalf("trusted peer identity header mutation = %v, want %v", foundIdentity, tc.wantIdentity)
				}
			}
			// TCP uses a different ext_authz API and must retain its existing filter pair.
			if tc.mesh == meshConfigGRPC && len(b.BuildTCP()) != 2 {
				t.Fatal("unexpected change to TCP CUSTOM filters")
			}
		})
	}
}
