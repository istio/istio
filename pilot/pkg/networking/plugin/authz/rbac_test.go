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

package authz

import (
	"reflect"
	"testing"

	policy "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	"github.com/envoyproxy/go-control-plane/envoy/type"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestConvertRbacRulesToFilterConfig(t *testing.T) {
	roles := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-1"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"prefix.*", "*.suffix", "service"},
						Methods:  []string{"GET", "POST"},
						Paths:    []string{"*/suffix", "/prefix*", "/exact", "*"},
					},
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-2"},
			Spec: &rbacproto.ServiceRole{
				Rules: []*rbacproto.AccessRule{
					{
						Services: []string{"*"},
						Constraints: []*rbacproto.AccessRule_Constraint{
							{Key: "key1", Values: []string{"prefix*", "*suffix"}},
							{Key: "key2", Values: []string{"simple", "*"}},
						},
					},
				},
			},
		},
	}
	bindings := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-1"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						User: "user",
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-1",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "service-role-binding-2"},
			Spec: &rbacproto.ServiceRoleBinding{
				Subjects: []*rbacproto.Subject{
					{
						Properties: map[string]string{
							"key": "value",
						},
					},
				},
				RoleRef: &rbacproto.RoleRef{
					Kind: "ServiceRole",
					Name: "service-role-2",
				},
			},
		},
	}

	policy1 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Paths: []*envoy_type.StringMatch{
				{MatchPattern: &envoy_type.StringMatch_Suffix{"/suffix"}},
				{MatchPattern: &envoy_type.StringMatch_Prefix{"/prefix"}},
				{MatchPattern: &envoy_type.StringMatch_Simple{"/exact"}},
				{MatchPattern: &envoy_type.StringMatch_Regex{"*"}}},
			Methods: []string{
				"GET", "POST",
			},
		},
		},
		Principals: []*policy.Principal{
			{Authenticated: &policy.Principal_Authenticated{Name: "user"}},
		},
	}

	policy2 := &policy.Policy{
		Permissions: []*policy.Permission{{
			Conditions: []*policy.Permission_Condition{
				{
					ConditionSpec: &policy.Permission_Condition_Header{
						Header: &policy.MapEntryMatch{
							Key: "key1",
							Values: []*envoy_type.StringMatch{
								{MatchPattern: &envoy_type.StringMatch_Prefix{"prefix"}},
								{MatchPattern: &envoy_type.StringMatch_Suffix{"suffix"}}}},
					},
				},
				{
					ConditionSpec: &policy.Permission_Condition_Header{
						Header: &policy.MapEntryMatch{
							Key: "key2",
							Values: []*envoy_type.StringMatch{
								{MatchPattern: &envoy_type.StringMatch_Simple{"simple"}},
								{MatchPattern: &envoy_type.StringMatch_Regex{"*"}}}},
					},
				},
			}},
		},
		Principals: []*policy.Principal{{
			Attributes: []*policy.Principal_Attribute{
				{
					AttributeSpec: &policy.Principal_Attribute_Header{
						Header: &policy.MapEntryMatch{
							Key: "key",
							Values: []*envoy_type.StringMatch{
								{MatchPattern: &envoy_type.StringMatch_Simple{"value"}},
							}},
					},
				},
			},
		},
		},
	}

	expectRbac1 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-1": policy1,
			"service-role-2": policy2,
		},
	}
	expectRbac2 := &policy.RBAC{
		Action: policy.RBAC_ALLOW,
		Policies: map[string]*policy.Policy{
			"service-role-2": policy2,
		},
	}
	testCases := []struct {
		name    string
		service string
		rbac    *policy.RBAC
	}{
		{
			name:    "prefix matched service",
			service: "prefix.service",
			rbac:    expectRbac1,
		},
		{
			name:    "suffix matched service",
			service: "service.suffix",
			rbac:    expectRbac1,
		},
		{
			name:    "exact matched service",
			service: "service",
			rbac:    expectRbac1,
		},
		{
			name:    "* matched service",
			service: "unknown",
			rbac:    expectRbac2,
		},
	}

	for _, tc := range testCases {
		rbac, err := convertRbacRulesToFilterConfig(tc.service, roles, bindings)
		if rbac == nil || err != nil {
			t.Errorf("failed to convert rbac rules to filter config: %v", err)
		}
		if !reflect.DeepEqual(*tc.rbac, *rbac) {
			t.Errorf("%s want:\n%v\nbut got:\n%v", tc.name, *tc.rbac, *rbac)
		}
	}
}
