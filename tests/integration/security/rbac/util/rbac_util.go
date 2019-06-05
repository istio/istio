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

package util

import (
	"fmt"
	"net/http"
	"strings"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/tests/integration/security/util/connection"
)

type TestCase struct {
	Request       connection.Checker
	ExpectAllowed bool
	Jwt           string
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
// *** For gRPC: gRPC permission denied error message.
func (tc TestCase) CheckRBACRequest() error {
	req := tc.Request

	headers := make(http.Header)
	if len(tc.Jwt) > 0 {
		headers.Add("Authorization", "Bearer "+tc.Jwt)
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
		if req.Options.PortName == "tcp" || req.Options.PortName == "grpc" {
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
