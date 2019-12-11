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

package v1alpha1

import (
	"testing"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestGetMutualTLSMode(t *testing.T) {
	cases := []struct {
		name     string
		in       *authn.Policy
		expected model.MutualTLSMode
	}{
		{
			name:     "Null policy",
			in:       nil,
			expected: model.MTLSDisable,
		},
		{
			name:     "Empty policy",
			in:       &authn.Policy{},
			expected: model.MTLSDisable,
		},
		{
			name: "Policy with default mTLS mode",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: model.MTLSStrict,
		},
		{
			name: "Policy with strict mTLS mode",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{
						Mtls: &authn.MutualTls{
							Mode: authn.MutualTls_STRICT,
						},
					},
				}},
			},
			expected: model.MTLSStrict,
		},
		{
			name: "Policy with permissive mTLS mode",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{
						Mtls: &authn.MutualTls{
							Mode: authn.MutualTls_PERMISSIVE,
						},
					},
				}},
			},
			expected: model.MTLSPermissive,
		},
		{
			name: "Policy with multi peer methods",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{},
					},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{
							Mtls: &authn.MutualTls{},
						},
					},
				},
			},
			expected: model.MTLSStrict,
		},
		{
			name: "Policy with non-mtls peer method",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{
					{
						Params: &authn.PeerAuthenticationMethod_Jwt{},
					},
				},
			},
			expected: model.MTLSDisable,
		},
	}
	for _, c := range cases {
		if got := GetMutualTLSMode(c.in); got != c.expected {
			t.Errorf("%s: isStrictMutualTLS(%v): got(%v) != want(%v)\n", c.name, c.in, got, c.expected)
		}
	}
}
