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

package v1beta1

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	securityPb "istio.io/api/security/v1beta1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

// TODO(pitlv2109): Add unit tests with trust domain aliases.
func TestV1beta1Generator_Generate(t *testing.T) {
	testCases := []struct {
		name           string
		denyPolicies   []model.AuthorizationPolicyConfig
		wantDenyRules  map[string][]string
		allowPolicies  []model.AuthorizationPolicyConfig
		wantAllowRules map[string][]string
		forTCPFilter   bool
	}{
		{
			name: "one policy",
			allowPolicies: []model.AuthorizationPolicyConfig{
				{
					Name:                "default",
					Namespace:           "foo",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_ALLOW),
				},
			},
			wantAllowRules: map[string][]string{
				"ns[foo]-policy[default]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
			},
		},
		{
			name: "allow policies",
			allowPolicies: []model.AuthorizationPolicyConfig{
				{
					Name:                "default",
					Namespace:           "foo",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_ALLOW),
				},
				{
					Name:                "default",
					Namespace:           "istio-system",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_ALLOW),
				},
			},
			wantAllowRules: map[string][]string{
				"ns[foo]-policy[default]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
				"ns[istio-system]-policy[default]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
			},
		},
		{
			name: "deny policies",
			denyPolicies: []model.AuthorizationPolicyConfig{
				{
					Name:                "default",
					Namespace:           "foo",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_DENY),
				},
				{
					Name:                "default",
					Namespace:           "istio-system",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_DENY),
				},
			},
			wantDenyRules: map[string][]string{
				"ns[foo]-policy[default]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
				"ns[istio-system]-policy[default]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
			},
		},
		{
			name: "allow and deny policies",
			denyPolicies: []model.AuthorizationPolicyConfig{
				{
					Name:                "default-deny",
					Namespace:           "foo",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_DENY),
				},
				{
					Name:                "default-deny",
					Namespace:           "istio-system",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_DENY),
				},
			},
			wantDenyRules: map[string][]string{
				"ns[foo]-policy[default-deny]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
				"ns[istio-system]-policy[default-deny]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
			},
			allowPolicies: []model.AuthorizationPolicyConfig{
				{
					Name:                "default-allow",
					Namespace:           "foo",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_ALLOW),
				},
				{
					Name:                "default-allow",
					Namespace:           "istio-system",
					AuthorizationPolicy: policy.SimpleAuthorizationProto("default", securityPb.AuthorizationPolicy_ALLOW),
				},
			},
			wantAllowRules: map[string][]string{
				"ns[foo]-policy[default-allow]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
				"ns[istio-system]-policy[default-allow]-rule[0]": {
					policy.AuthzPolicyTag("default"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(trustdomain.NewTrustDomainBundle("", nil), tc.denyPolicies, tc.allowPolicies)
			if g == nil {
				t.Fatal("failed to create generator")
			}

			gotDeny, gotAllow := g.Generate(tc.forTCPFilter)
			if err := policy.Verify(gotDeny.GetRules(), tc.wantDenyRules, false, true /* wantDeny */); err != nil {
				t.Fatalf("%s\n%s", err, spew.Sdump(gotDeny))
			}
			if err := policy.Verify(gotAllow.GetRules(), tc.wantAllowRules, false, false /* wantDeny */); err != nil {
				t.Fatalf("%s\n%s", err, spew.Sdump(gotAllow))
			}
		})
	}
}
