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

package authn

import (
	"fmt"

	"istio.io/istio/tests/integration/security/util/connection"
)

type TestCase struct {
	Request             connection.Checker
	ExpectAuthenticated bool
}

func getErrorMessage(req connection.Checker, expect, actual string) error {
	return fmt.Errorf("%s to %s:%s using %s: expected %s, got %s",
		req.From.Config().Service, req.Options.Target.Config().Service, req.Options.PortName, req.Options.Scheme, expect, actual)
}

// CheckAuthn checks a request based on ExpectAuthenticated (true: resp code 200; false: resp code 401 ).
func (c *TestCase) CheckAuthn() error {
	results, err := c.Request.From.Call(c.Request.Options)
	if c.ExpectAuthenticated {
		if err == nil {
			err = results.CheckOK()
		}
		if err != nil {
			return getErrorMessage(c.Request, "authenticated (code 200)", err.Error())
		}
	} else {
		// Expect 401
		if err != nil {
			return getErrorMessage(c.Request, "unauthenticated (code 401)", err.Error())
		}
		errMsg := ""
		if len(results) == 0 {
			errMsg = "no response"
		}
		if results[0].Code != "401" {
			errMsg = fmt.Sprintf("code %s", results[0].Code)
		}
		if errMsg != "" {
			return getErrorMessage(c.Request, "unauthenticated (code 401)", errMsg)
		}
	}
	return nil
}
