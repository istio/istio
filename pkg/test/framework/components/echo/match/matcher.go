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

package match

import (
	"errors"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// Matcher is used to filter matching instances
type Matcher func(echo.Instance) bool

// GetMatches returns the subset of echo.Instances that match this Matcher.
func (m Matcher) GetMatches(i echo.Instances) echo.Instances {
	out := make(echo.Instances, 0)
	for _, i := range i {
		if m(i) {
			out = append(out, i)
		}
	}
	return out
}

// GetServiceMatches returns the subset of echo.Services that match this Matcher.
func (m Matcher) GetServiceMatches(services echo.Services) echo.Services {
	out := make(echo.Services, 0)
	for _, s := range services {
		if len(s) > 0 && m(s[0]) {
			out = append(out, s)
		}
	}
	return out
}

// First finds the first Instance that matches the Matcher.
func (m Matcher) First(i echo.Instances) (echo.Instance, error) {
	for _, i := range i {
		if m(i) {
			return i, nil
		}
	}

	return nil, errors.New("found 0 matching echo instances")
}

// FirstOrFail calls First and then fails the test if an error occurs.
func (m Matcher) FirstOrFail(t test.Failer, i echo.Instances) echo.Instance {
	res, err := m.First(i)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Any indicates whether any echo.Instance matches this matcher.
func (m Matcher) Any(i echo.Instances) bool {
	for _, i := range i {
		if m(i) {
			return true
		}
	}
	return false
}

func (m Matcher) All(i echo.Instances) bool {
	for _, i := range i {
		if !m(i) {
			return false
		}
	}

	return true
}
