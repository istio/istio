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

	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	DenyHTTPRespCode = "403"
)

type TestCase struct {
	Request       connection.Checker
	ExpectAllowed bool
	Jwt           string
	RejectionCode string
}

// CheckRBACRequest checks if a request is successful under RBAC policies.
// Under RBAC policies, a request is consider successful if:
// * If the policy is deny:
// *** For HTTP: response code is same as the tc.RejectionCode.
// *** For TCP: EOF error
// * If the policy is allow:
// *** Response code is 200
func (tc TestCase) Check() error {
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
			return fmt.Errorf("%s to %s:%s%s using %s: expected allow but was denied: %v",
				req.From.Config().Service,
				req.Options.Target.Config().Service,
				req.Options.PortName,
				req.Options.Path,
				req.Options.Scheme,
				err)
		}
	} else {
		if err != nil && !strings.Contains(err.Error(), "EOF") {
			return fmt.Errorf("connection error with %v", err)
		}

		if req.Options.PortName == "tcp" {
			if err != nil && !strings.Contains(err.Error(), "EOF") {
				return fmt.Errorf("%s to %s:%s%s using %s: expected deny with EOF error, actually %v",
					req.From.Config().Service,
					req.Options.Target.Config().Service,
					req.Options.PortName,
					req.Options.Path,
					req.Options.Scheme,
					err)
			}
		} else {
			if !(len(resp) > 0 && resp[0].Code == tc.RejectionCode) {
				return fmt.Errorf("%s to %s:%s%s using %s: expected deny, actually allow",
					req.From.Config().Service,
					req.Options.Target.Config().Service,
					req.Options.PortName,
					req.Options.Path,
					req.Options.Scheme)
			}
		}
	}
	// Success
	return nil
}
