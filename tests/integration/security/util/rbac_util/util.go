//go:build integ
// +build integ

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
	"time"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util/connection"
)

// ExpectContains specifies the expected value to be found in the HTTP responses. Every value must be found in order to
// to make the test pass. Every NotValue must not be found in order to make the test pass.
type ExpectContains struct {
	Key       string
	Values    []string
	NotValues []string
}

type TestCase struct {
	NamePrefix         string
	Request            connection.Checker
	ExpectAllowed      bool
	ExpectHTTPResponse []ExpectContains
	Jwt                string
	Headers            map[string]string
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

func checkValues(i int, response echo.Response, want []ExpectContains) error {
	for _, w := range want {
		key := w.Key
		for _, value := range w.Values {
			if !strings.Contains(response.RawResponse[key], value) {
				return fmt.Errorf("response[%d]: HTTP code %s, want value %s in key %s, but not found in %s",
					i, response.Code, value, key, response.RawResponse)
			}
		}
		for _, value := range w.NotValues {
			if strings.Contains(response.RawResponse[key], value) {
				return fmt.Errorf("response[%d]: HTTP code %s, do not want value %s in key %s, but found in %s",
					i, response.Code, value, key, response.RawResponse)
			}
		}
	}
	return nil
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
		checker := func(rs echo.Responses, err error) error {
			if err == nil {
				err = check.OK().Check(rs, nil)
			}
			if err != nil {
				return getError(req, "allow with code 200", fmt.Sprintf("error: %v", err))
			}

			for i, r := range rs {
				if err := checkValues(i, r, tc.ExpectHTTPResponse); err != nil {
					return err
				}
			}

			if req.DestClusters.IsMulticluster() {
				return check.ReachedClusters(req.DestClusters).Check(rs, err)
			}
			return nil
		}
		return checker(resp, err)
	}

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
		return nil
	}

	if err != nil {
		return getError(req, "deny with code 403", fmt.Sprintf("error: %v", err))
	}
	var result string
	if len(resp) == 0 {
		result = "no response"
	} else if resp[0].Code != echo.StatusCodeForbidden {
		result = resp[0].Code
	}
	if result != "" {
		return getError(req, "deny with code 403", result)
	}

	for i, r := range resp {
		if err := checkValues(i, r, tc.ExpectHTTPResponse); err != nil {
			return err
		}
	}
	return nil
}

func RunRBACTest(ctx framework.TestContext, cases []TestCase) {
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
		ctx.NewSubTest(testName).Run(func(t framework.TestContext) {
			// Current source ip based authz test cases are not required in multicluster setup
			// because cross-network traffic will lose the origin source ip info
			if strings.Contains(testName, "source-ip") && t.Clusters().IsMulticluster() {
				t.Skip()
			}
			retry.UntilSuccessOrFail(t, tc.CheckRBACRequest,
				retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
		})
	}
}
