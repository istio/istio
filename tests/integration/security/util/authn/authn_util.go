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

package authn

import (
	"fmt"
	"net/http"
	"strings"

	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/tests/integration/security/util/connection"
)

type TestCase struct {
	Name               string
	Request            connection.Checker
	ExpectResponseCode string
	// Use empty value to express the header with such key must not exist.
	ExpectHeaders map[string]string
}

func (c *TestCase) String() string {
	return fmt.Sprintf("%s to %s%s expected code %s, headers %v",
		c.Request.From.Config().Service,
		c.Request.Options.Target.Config().Service,
		c.Request.Options.Path,
		c.ExpectResponseCode,
		c.ExpectHeaders)
}

// CheckAuthn checks a request based on ExpectResponseCode.
func (c *TestCase) CheckAuthn() error {
	results, err := c.Request.From.Call(c.Request.Options)
	if len(results) == 0 {
		return fmt.Errorf("%s: no response", c)
	}
	if results[0].Code != c.ExpectResponseCode {
		return fmt.Errorf("%s: got response code %s, err %v", c, results[0].Code, err)
	}
	// Checking if echo backend see header with the given value by finding them in response body
	// (given the current behavior of echo convert all headers into key=value in the response body)
	for k, v := range c.ExpectHeaders {
		matcher := fmt.Sprintf("%s=%s", k, v)
		if len(v) == 0 {
			if strings.Contains(results[0].Body, matcher) {
				return fmt.Errorf("%s: expect header %s does not exist, got response\n%s", c, k, results[0].Body)
			}
		} else {
			if !strings.Contains(results[0].Body, matcher) {
				return fmt.Errorf("%s: expect header %s=%s in body, got response\n%s", c, k, v, results[0].Body)
			}
		}
	}
	return nil
}

// CheckIngress checks a request for the ingress gateway.
func CheckIngress(ingr ingress.Instance, host string, path string, token string, expectResponseCode int) error {
	endpointAddress := ingr.HTTPAddress()
	opts := ingress.CallOptions{
		Host:     host,
		Path:     path,
		CallType: ingress.PlainText,
		Address:  endpointAddress,
	}
	if len(token) != 0 {
		opts.Headers = http.Header{
			"Authorization": []string{
				fmt.Sprintf("Bearer %s", token),
			},
		}
	}
	response, err := ingr.Call(opts)

	if response.Code != expectResponseCode {
		return fmt.Errorf("got response code %d, err %s", response.Code, err)
	}
	return nil
}
