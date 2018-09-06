//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"testing"

	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

func TestHTTP(t *testing.T) {
	framework.Requires(t, dependency.Apps)

	testHTTP(t)
}

func TestHTTPKubernetes(t *testing.T) {
	framework.Requires(t, dependency.Kubernetes, dependency.Apps)

	testHTTP(t)
}

func TestHTTPLocal(t *testing.T) {
	framework.Requires(t, dependency.Local, dependency.Apps)

	testHTTP(t)
}

func testHTTP(t *testing.T) {
	t.Helper()

	env := framework.AcquireEnvironment(t)

	a := env.GetAppOrFail("a", t)
	b := env.GetAppOrFail("b", t)
	c := env.GetAppOrFail("c", t)

	// Send requests to all of the HTTP endpoints.
	apps := []environment.DeployedApp{a, b, c}
	for _, src := range apps {
		for _, target := range apps {
			if src != target {
				endpoints := target.EndpointsForProtocol(model.ProtocolHTTP)
				for _, endpoint := range endpoints {
					testName := fmt.Sprintf("%s_%s[%s]", src.Name(), target.Name(), endpoint.Name())
					t.Run(testName, func(t *testing.T) {
						results := src.CallOrFail(endpoint, environment.AppCallOptions{}, t)
						if !results[0].IsOK() {
							t.Fatalf("HTTP Request unsuccessful: %s", results[0].Body)
						}
					})
				}
			}
		}
	}
}

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	framework.Run("pilot_test", m)
}
