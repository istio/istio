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
	Name               string
	Request            connection.Checker
	ExpectResponseCode string
}

func (c *TestCase) String() string {
	return fmt.Sprintf("%s to %s%s expected %s",
		c.Request.From.Config().Service,
		c.Request.Options.Target.Config().Service,
		c.Request.Options.Path,
		c.ExpectResponseCode)
}

// CheckAuthn checks a request based on ExpectResponseCode.
func (c *TestCase) CheckAuthn() error {
	results, err := c.Request.From.Call(c.Request.Options)
	if len(results) == 0 {
		return fmt.Errorf("%s: no response", c)
	}
	if results[0].Code != c.ExpectResponseCode {
		return fmt.Errorf("%s: got response code %s, err %s", c, results[0].Code, err)
	}
	return nil
}
