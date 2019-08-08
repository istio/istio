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

package auth

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

type cases struct {
	testName string
	input    []string
	expected string
	errMsg   string
}

func TestCheckAndReport(t *testing.T) {
	cases := []cases{
		{
			testName: "no policy found",
			input:    []string{"./testdata/validator/service-entry.yaml"},
			expected: ValidButNoRBACFound,
		},
		{
			testName: "no binding found",
			input:    []string{"./testdata/validator/unused-role.yaml"},
			errMsg:   BindingMissing,
		},
		{
			testName: "no role found",
			input:    []string{"./testdata/validator/notfound-role-in-binding.yaml"},
			errMsg:   RoleMissing,
		},
		{
			testName: "good rbac file",
			input:    []string{"./testdata/validator/rbac-policies.yaml"},
			expected: GetPolicyValidReport(),
		},
		{
			testName: "bad rbac file",
			input:    []string{"./testdata/validator/unused-role.yaml", "./testdata/validator/notfound-role-in-binding.yaml"},
			expected: fmt.Sprintf("%s%s",
				GetRoleNotFoundReport("some-role", "bind-service-viewer", "default"),
				GetRoleNotUsedReport("unused-role", "default")),
		},
	}
	for _, tc := range cases {
		validator := Validator{
			PolicyFiles:          tc.input,
			RoleKeyToServiceRole: make(map[string]model.Config),
		}
		err := validator.CheckAndReport()
		if err != nil {
			if tc.errMsg == "" {
				t.Errorf("test %q failed with error %v", tc.testName, err)
			}
			if tc.errMsg != err.Error() {
				t.Errorf("test %q failed. \nExpected \n%sGot\n%s", tc.testName, tc.errMsg, err.Error())
			}
		}
		gotContent := validator.Report.String()
		if tc.expected != gotContent {
			t.Errorf("test %q failed. \nExpected \n%sGot\n%s", tc.testName, tc.expected, gotContent)
		}
	}
}
