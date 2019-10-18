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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
)

// TODO(pitlv2109): Add unit tests with trust domain aliases.
func TestV1beta1Generator_Generate(t *testing.T) {
	testCases := []struct {
		name               string
		policies           []model.Config
		trustDomain        string
		trustDomainAliases []string
		wantRules          map[string][]string
		forTCPFilter       bool
	}{
		{
			name: "no policy",
		},
		{
			name: "one policy",
			policies: []model.Config{
				*policy.SimpleAuthzPolicy("default", "foo",
					[]string{policy.CustomPrincipal("cluster.local", "foo", "bar")}),
			},
			trustDomain:        "td",
			trustDomainAliases: []string{"cluster.local"},
			wantRules: map[string][]string{
				"ns[foo]-policy[default]-rule[0]": {
					policy.CustomPrincipal("td", "foo", "bar"),
					policy.CustomPrincipal("cluster.local", "foo", "bar"),
				},
			},
		},
		{
			name: "two policies",
			policies: []model.Config{
				*policy.SimpleAuthzPolicy("default", "foo",
					[]string{policy.CustomPrincipal("cluster.local", "foo", "bar")}),
				*policy.SimpleAuthzPolicy("default", "istio-system",
					[]string{policy.CustomPrincipal("td1", "foo", "boo")}),
			},
			trustDomain:        "td2",
			trustDomainAliases: []string{"cluster.local", "td1"},
			wantRules: map[string][]string{
				"ns[foo]-policy[default]-rule[0]": {
					policy.CustomPrincipal("td2", "foo", "bar"),
					policy.CustomPrincipal("cluster.local", "foo", "bar"),
					policy.CustomPrincipal("td1", "foo", "bar"),
				},
				"ns[istio-system]-policy[default]-rule[0]": {
					policy.CustomPrincipal("td2", "foo", "boo"),
					policy.CustomPrincipal("cluster.local", "foo", "boo"),
					policy.CustomPrincipal("td1", "foo", "boo"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(tc.trustDomain, tc.trustDomainAliases, tc.policies)
			if g == nil {
				t.Fatal("failed to create generator")
			}

			got := g.Generate(tc.forTCPFilter)
			if got.GetRules() == nil {
				t.Fatal("rule must not be nil")
			}
			if err := policy.Verify(got.GetRules(), tc.wantRules); err != nil {
				t.Fatalf("%s\n%s", err, spew.Sdump(got))
			}
		})
	}
}
