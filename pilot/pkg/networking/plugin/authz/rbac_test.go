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
	"testing"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"reflect"
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

	policy1 := &Policy{
		Permissions: []*Permission{{
			Paths: []*StringMatch{
				{MatchPattern: &StringMatch_Suffix{"/suffix"}},
				{MatchPattern: &StringMatch_Prefix{"/prefix"}},
				{MatchPattern: &StringMatch_Simple{"/exact"}},
				{MatchPattern: &StringMatch_Regex{"*"}}},
			Methods: []string{
				"GET", "POST",
			},
		},
		},
		Principals: []*Principal{
			{Authenticated: &Principal_Authenticated{Name: "user"}},
		},
	}

	policy2 := &Policy{
		Permissions: []*Permission{{
			Conditions: []*Permission_Condition{
				{
					ConditionSpec: &Permission_Condition_Header{
						Header: &MapEntryMatch{
							Key: "key1",
							Values: []*StringMatch{
								{MatchPattern: &StringMatch_Prefix{"prefix"}},
								{MatchPattern: &StringMatch_Suffix{"suffix"}}}},
					},
				},
				{
					ConditionSpec: &Permission_Condition_Header{
						Header: &MapEntryMatch{
							Key: "key2",
							Values: []*StringMatch{
								{MatchPattern: &StringMatch_Simple{"simple"}},
								{MatchPattern: &StringMatch_Regex{"*"}}}},
					},
				},
			}},
		},
		Principals: []*Principal{{
			Attributes: []*Principal_Attribute{
				{
					AttributeSpec: &Principal_Attribute_Header{
						Header: &MapEntryMatch{
							Key: "key",
							Values: []*StringMatch{
								{MatchPattern: &StringMatch_Simple{"value"}},
							}},
					},
				},
			},
		},
		},
	}

	expectRbac1 := &RBAC{
		Action: RBAC_ALLOW,
		Policies: map[string]*Policy{
			"service-role-1": policy1,
			"service-role-2": policy2,
		},
	}
	expectRbac2 := &RBAC{
		Action: RBAC_ALLOW,
		Policies: map[string]*Policy{
			"service-role-2": policy2,
		},
	}
	testCases := []struct {
		name    string
		service string
		rbac    *RBAC
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
