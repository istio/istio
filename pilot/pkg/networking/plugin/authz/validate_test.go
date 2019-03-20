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
)

func TestValidateRule(t *testing.T) {
	testCases := []struct {
		name    string
		rule    *rbacproto.AccessRule
		success bool
	}{
		{
			name:    "nil rule",
			success: true,
		},
		{
			name: "rule with path",
			rule: &rbacproto.AccessRule{
				Paths: []string{"/"},
			},
		},
		{
			name: "rule with method",
			rule: &rbacproto.AccessRule{
				Methods: []string{"GET"},
			},
		},
		{
			name: "rule with unsupported constraint",
			rule: &rbacproto.AccessRule{
				Constraints: []*rbacproto.AccessRule_Constraint{
					{
						Key:    attrDestIP,
						Values: []string{"1.2.3.4"},
					},
					{
						Key:    attrRequestHeader,
						Values: []string{"TOKEN"},
					},
				},
			},
		},
		{
			name: "good rule",
			rule: &rbacproto.AccessRule{
				Constraints: []*rbacproto.AccessRule_Constraint{
					{
						Key:    attrDestIP,
						Values: []string{"1.2.3.4"},
					},
					{
						Key:    attrDestPort,
						Values: []string{"80"},
					},
				},
			},
			success: true,
		},
	}
	for _, tc := range testCases {
		ret := validateRuleForTCPFilter(tc.rule)
		if tc.success != (ret == nil) {
			t.Errorf("%s: expecting %v bot got %v", tc.name, tc.success, ret)
		}
	}
}

func TestValidateBinding(t *testing.T) {
	testCases := []struct {
		name     string
		bindings []*rbacproto.ServiceRoleBinding
		success  bool
	}{
		{
			name:    "empty bindings",
			success: true,
		},
		{
			name: "binding with group",
			bindings: []*rbacproto.ServiceRoleBinding{
				{
					Subjects: []*rbacproto.Subject{
						{Group: "group"},
					},
				},
			},
		},
		{
			name: "binding with unsupported property",
			bindings: []*rbacproto.ServiceRoleBinding{
				{
					Subjects: []*rbacproto.Subject{
						{Properties: map[string]string{attrRequestPresenter: "ns"}},
					},
				},
			},
		},
		{
			name: "good binding",
			bindings: []*rbacproto.ServiceRoleBinding{
				{
					Subjects: []*rbacproto.Subject{
						{User: "user", Properties: map[string]string{attrSrcNamespace: "ns", attrSrcPrincipal: "p"}},
					},
				},
			},
			success: true,
		},
	}
	for _, tc := range testCases {
		ret := validateBindingsForTCPFilter(tc.bindings)
		if tc.success != (ret == nil) {
			t.Errorf("%s: expecting %v bot got %v", tc.name, tc.success, ret)
		}
	}
}
