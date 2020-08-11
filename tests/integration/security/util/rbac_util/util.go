// Copyright Istio Authors
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

package rbac

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
)

type TestCase struct {
	NamePrefix    string
	Request       connection.Checker
	ExpectAllowed bool
	Jwt           string
	Headers       map[string]string
}

func getError(req connection.Checker, expect, actual string) error {
	return fmt.Errorf("%s to %s:%s%s: expect %s, got: %s",
		req.From.Config().Service,
		req.Options.Target.Config().Service,
		req.Options.PortName,
		req.Options.Path,
		expect,
		actual)
}

// CheckRBACRequest checks if a request is successful under RBAC policies.
// Under RBAC policies, a request is consider successful if:
// * If the policy is allow:
// *** Response code is 200
// * If the policy is deny:
// *** For HTTP: response code is 403.
// *** For TCP: EOF error
func (tc TestCase) CheckRBACRequest() error {
	req := tc.Request

	headers := make(http.Header)
	if len(tc.Jwt) > 0 {
		headers.Add("Authorization", "Bearer "+tc.Jwt)
	}
	for k, v := range tc.Headers {
		headers.Add(k, v)
	}
	tc.Request.Options.Headers = headers

	resp, err := req.From.Call(tc.Request.Options)

	if tc.ExpectAllowed {
		if err == nil {
			err = resp.CheckOK()
		}
		if err != nil {
			return getError(req, "allow with code 200", fmt.Sprintf("error: %v", err))
		}
	} else {
		if strings.HasPrefix(req.Options.PortName, "tcp") || req.Options.PortName == "grpc" {
			expectedErrMsg := "EOF" // TCP deny message.
			if req.Options.PortName == "grpc" {
				expectedErrMsg = "rpc error: code = PermissionDenied desc = RBAC: access denied"
			}
			if err == nil || !strings.Contains(err.Error(), expectedErrMsg) {
				expect := fmt.Sprintf("deny with %s error", expectedErrMsg)
				actual := fmt.Sprintf("error: %v", err)
				return getError(req, expect, actual)
			}
		} else {
			if err != nil {
				return getError(req, "deny with code 403", fmt.Sprintf("error: %v", err))
			}
			var result string
			if len(resp) == 0 {
				result = "no response"
			} else if resp[0].Code != response.StatusCodeForbidden {
				result = resp[0].Code
			}
			if result != "" {
				return getError(req, "deny with code 403", result)
			}
		}
	}
	return nil
}

func RunRBACTest(t *testing.T, cases []TestCase) {
	for _, tc := range cases {
		want := "deny"
		if tc.ExpectAllowed {
			want = "allow"
		}
		testName := fmt.Sprintf("%s%s->%s:%s%s[%s]",
			tc.NamePrefix,
			tc.Request.From.Config().Service,
			tc.Request.Options.Target.Config().Service,
			tc.Request.Options.PortName,
			tc.Request.Options.Path,
			want)
		t.Run(testName, func(t *testing.T) {
			retry.UntilSuccessOrFail(t, tc.CheckRBACRequest,
				retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
		})
	}
}
