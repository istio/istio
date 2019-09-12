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
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schemas"
)

func TestGetMutualTLSMode(t *testing.T) {
	cases := []struct {
		name     string
		in       *authn.Policy
		expected MutualTLSMode
	}{
		{
			name:     "Null policy",
			in:       nil,
			expected: MTLSDisable,
		},
		{
			name:     "Empty policy",
			in:       &authn.Policy{},
			expected: MTLSDisable,
		},
		{
			name: "Policy with default mTLS mode",
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			expected: MTLSStrict,
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
			expected: MTLSStrict,
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
			expected: MTLSPermissive,
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
			expected: MTLSStrict,
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
			expected: MTLSDisable,
		},
	}
	for _, c := range cases {
		if got := getMutualTLSMode(c.in); got != c.expected {
			t.Errorf("%s: isStrictMutualTLS(%v): got(%v) != want(%v)\n", c.name, c.in, got, c.expected)
		}
	}
}

func TestGetServiceMutualTLSMode(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(schemas.Istio))
	authNPolicies := map[string]*authn.Policy{
		constants.DefaultAuthenticationPolicyName: {},
		"mtls-strict": {
			Targets: []*authn.TargetSelector{{
				Name: "strict-svc",
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			}},
		},
		"mtls-permissive": {
			Targets: []*authn.TargetSelector{{
				Name: "permissive-svc",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{
					Mtls: &authn.MutualTls{
						Mode: authn.MutualTls_PERMISSIVE,
					},
				},
			}},
		},
		"mtls-disable": {
			Targets: []*authn.TargetSelector{{
				Name: "disable-svc",
			}},
		},
	}
	for key, value := range authNPolicies {
		cfg := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schemas.AuthenticationPolicy.Type,
				Name:      key,
				Group:     "authentication",
				Version:   "v1alpha1",
				Namespace: "default",
				Domain:    "cluster.local",
			},
			Spec: value,
		}
		if _, err := store.Create(cfg); err != nil {
			t.Error(err)
		}
	}

	cases := []struct {
		hostname  host.Name
		namespace string
		port      int
		expected  MutualTLSMode
	}{
		{
			hostname:  "strict-svc.default.svc.cluster.local",
			namespace: "default",
			port:      80,
			expected:  MTLSStrict,
		},
		{
			hostname:  "permissive-svc.default.svc.cluster.local",
			namespace: "default",
			port:      80,
			expected:  MTLSPermissive,
		},
		{
			hostname:  "permissive-svc.default.svc.cluster.local",
			namespace: "default",
			port:      90,
			expected:  MTLSDisable,
		},
		{
			hostname:  "disable-svc.default.svc.cluster.local",
			namespace: "default",
			port:      80,
			expected:  MTLSDisable,
		},
		{
			hostname:  "strict-svc.another-namespace.svc.cluster.local",
			namespace: "another-namespace",
			port:      80,
			expected:  MTLSDisable,
		},
	}

	for i, c := range cases {
		port := &model.Port{Port: c.port}
		service := &model.Service{
			Hostname:   c.hostname,
			Attributes: model.ServiceAttributes{Namespace: c.namespace},
		}
		if got := GetServiceMutualTLSMode(store, service, port); got != c.expected {
			t.Errorf("%d. GetServiceMutualTLSMode for %s.%s:%d: got(%v) != want(%v)\n", i, c.hostname, c.namespace, c.port, got, c.expected)
		}
	}
}
