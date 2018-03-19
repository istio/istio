// Copyright 2018 Istio Authors
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

package model

import (
	"reflect"
	"testing"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestRequireTls(t *testing.T) {
	cases := []struct {
		in       authn.Policy
		expected bool
	}{
		{
			in:       authn.Policy{},
			expected: false,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: true,
		},
		{
			in: authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{},
				},
					{
						Params: &authn.PeerAuthenticationMethod_Mtls{},
					},
				},
			},
			expected: true,
		},
	}
	for _, c := range cases {
		if got := RequireTLS(&c.in); got != c.expected {
			t.Errorf("requireTLS(%v): got(%v) != want(%v)\n", c.in, got, c.expected)
		}
	}
}

func TestLegacyAuthenticationPolicyToPolicy(t *testing.T) {
	cases := []struct {
		in       meshconfig.AuthenticationPolicy
		expected *authn.Policy
	}{
		{
			in: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
			expected: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
		},
		{
			in:       meshconfig.AuthenticationPolicy_NONE,
			expected: nil,
		},
	}

	for _, c := range cases {
		if got := legacyAuthenticationPolicyToPolicy(c.in); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("legacyAuthenticationPolicyToPolicy(%v): got(%#v) != want(%#v)\n", c.in, got, c.expected)
		}
	}
}
