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

// Package showcase contains a test suite implementation example that showcases a particular/opinionated
// design sketch.
package showcase

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/label"
)

// Capturing TestMain allows us to:
// - Do cleanup before exit
// - process testing specific flags
func TestMain(m *testing.M) {
	// Tags all tests in the suite with this tag.
	test.SuiteTag(m, label.Networking)

	// Indicates that all tests in the suite requires a particular dependency.
	test.SuiteRequires(m, dependency.GKE)

	test.Run("showcase_test", m)
}

// Ignore this test with a reason. Uses Go's "Skip"
func TestIgnored(t *testing.T) {
	test.Ignore(t, "bad test")
}

// Test categorization can be used to filter out tests.
// Try running "go test -labels=networking" or "go test -labels=policy" etc.
func TestCategorization(t *testing.T) {
	test.Tag(t, label.Networking)
}

// Requirement checks and ensures that the specified requirement can be satisfied.
// In this case, the "Apps" and "Pilot" dependencies will be initialized (once per running suite) and be
// available for use.
func TestRequirement_Apps(t *testing.T) {
	test.Requires(t, dependency.Apps, dependency.Pilot)

}

// Require (specifically) a GKE cluster.
func TestRequirement_GKE(t *testing.T) {
	test.Requires(t, dependency.GKE) // We specifically need GKE

	// ....
}

func TestFull(t *testing.T) {
	test.Requires(t, dependency.Apps, dependency.Pilot, dependency.Mixer, dependency.PolicyBackend)

	// Environment is the main way to interact with Istio components and the testing apparatus. Environment
	// encapsulates the specifics (i.e. whether it is based on local processes, or cluster) but exposes a
	// uniform an API.
	env := test.GetEnvironment(t)

	// Configure the environment for this particular test.
	cfg := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
...
`
	env.Configure(cfg)

	// Get handles to the fake applications to drive the test.
	appa := env.GetAppOrFail("a", t)
	appt := env.GetAppOrFail("t", t)

	// Returns the fake policy backend that Mixer will use to check policies against.
	policyBe := env.GetPolicyBackendOrFail(t)

	// Prime the policy backend's behavior. It should deny all check requests.
	policyBe.DenyCheck()

	// Send requests to all of the HTTP endpoints. We expect the deny check to cause the HTTP requests to fail.
	endpoints := appt.EndpointsForProtocol(model.ProtocolHTTP)
	for _, endpoint := range endpoints {
		url := endpoint.MakeURL(false) + "/a"
		t.Run(url, func(t *testing.T) {
			result, err := appa.Call(url, 1, nil)
			if err != nil {
				t.Fatal(err)
			}

			// We do not expect check calls to fail
			if result.IsSuccess() {
				t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
			}
		})
	}
}
