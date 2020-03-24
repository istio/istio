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

package builder

import (
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
	authzModel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
)

func TestBuilder_BuildHTTPFilters(t *testing.T) {
	cases := []struct {
		name      string
		labels    labels.Collection
		namespace string
		policies  *model.AuthorizationPolicies
		want      []string
	}{
		{
			name:      "none",
			namespace: "a",
		},
		{
			name:      "not-matched",
			namespace: "b",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
		},
		{
			name:      "deny",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
			want: []string{"deny-bar"},
		},
		{
			name:      "allow",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.AllowPolicy("allow-bar", "a"),
			}, t),
			want: []string{"allow-bar"},
		},
		{
			name:      "deny-and-allow",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.AllowPolicy("allow-bar", "a"),
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
			want: []string{"deny-bar", "allow-bar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trustDomain := trustdomain.NewTrustDomainBundle("", nil)
			b := NewBuilder(trustDomain, tc.labels, tc.namespace, tc.policies)
			gotFilters := b.BuildHTTPFilters()
			if len(tc.want) != len(gotFilters) {
				t.Fatalf("want %d filters, got %d", len(tc.want), len(gotFilters))
			}
			for i, want := range tc.want {
				got := spew.Sdump(gotFilters[i])
				if !strings.Contains(got, want) {
					t.Errorf("want %s, got %s", want, got)
				}
				if gotFilters[i].Name != authzModel.RBACHTTPFilterName {
					t.Errorf("want %s, got %s", authzModel.RBACHTTPFilterName, gotFilters[i].Name)
				}
			}
		})
	}
}

func TestBuilder_BuildTCPFilters(t *testing.T) {
	cases := []struct {
		name      string
		labels    labels.Collection
		namespace string
		policies  *model.AuthorizationPolicies
		want      []string
	}{
		{
			name:      "none",
			namespace: "a",
		},
		{
			name:      "not-matched",
			namespace: "b",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
		},
		{
			name:      "deny",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
			want: []string{"deny-bar"},
		},
		{
			name:      "allow",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.AllowPolicy("allow-bar", "a"),
			}, t),
			want: []string{"allow-bar"},
		},
		{
			name:      "deny-and-allow",
			namespace: "a",
			policies: policy.NewAuthzPolicies([]*model.Config{
				policy.AllowPolicy("allow-bar", "a"),
				policy.DenyPolicy("deny-bar", "a"),
			}, t),
			want: []string{"deny-bar", "allow-bar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trustDomain := trustdomain.NewTrustDomainBundle("", nil)
			b := NewBuilder(trustDomain, tc.labels, tc.namespace, tc.policies)
			gotFilters := b.BuildTCPFilters()
			if len(tc.want) != len(gotFilters) {
				t.Fatalf("want %d filters, got %d", len(tc.want), len(gotFilters))
			}
			for i, want := range tc.want {
				got := spew.Sdump(gotFilters[i])
				if !strings.Contains(got, want) {
					t.Errorf("want %s, got %s", want, got)
				}
				if gotFilters[i].Name != authzModel.RBACTCPFilterName {
					t.Errorf("want %s, got %s", authzModel.RBACTCPFilterName, gotFilters[i].Name)
				}
			}
		})
	}
}
