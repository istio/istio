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

package zipkin

import (
	"fmt"
	"github.com/satori/go.uuid"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"net/http"
	"strings"
	"testing"
)

// An integration test version of the zipkin end-to-end test.
// This test sets up two applications with envoys that talk to zipkin, and verifies that a trace
// header set in a request is correctly propagated to the zipkin server.
// Note that the native version of this is only testing the framework's own pilot agent, and not the
// real one, and as such is mostly useless, but does verify envoy is doing the right thing.
func TestZipkin(t *testing.T) {
	ctx := framework.GetContext(t)

	// For now we require native environment, the zipkin test is not verified to work on kube yet.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment, &ids.Zipkin)

	// Require apps after we have started zipkin, because the pilot agent checks for zipkin presence.
	ctx.RequireOrSkip(t, lifecycle.Test, &ids.Apps)

	// Pull out the a and b apps so we can make a call between them, using the client trace id header.
	apps := components.GetApps(ctx, t)
	a := apps.GetAppOrFail("a", t)
	b := apps.GetAppOrFail("b", t)

	be := b.EndpointsForProtocol(model.ProtocolHTTP)[0]

	zip := components.GetZipkin(ctx, t)

	for traceCount := 0; traceCount < 5; traceCount++ {
		callOpts := components.AppCallOptions{}
		id := uuid.NewV4().String()
		callOpts.Headers = http.Header{}
		callOpts.Headers.Add("X-Client-Trace-Id", id)

		result := a.CallOrFail(be, callOpts, t)[0]
		if !result.IsOK() {
			t.Fatal("HTTP Request unsuccessful: ", result.Body)
		}

		// Traces take some time to propagate, so we use an Eventually to wait until traces are ready.
		test.Eventually(t, "Traces propagated to zipkin", func() bool {
			traces, err := zip.GetTracesById(id)
			if err != nil {
				t.Fatal("Error fetching traces: ", err)
			}
			if len(traces) > 0 {
				traceCount := strings.Count(fmt.Sprint(traces), id)
				if traceCount > 1 {
					return true
				}
			}
			return false
		})
	}
}

// What a_zipkin_test e2e test does:
// For 5 traces
//   Send a request from a to b with a trace header of X-Client-Trace-Id <uuid>
//     Retry until trace is successfully sent.
//   Make a request to the zipkin API for the trace with that ID
//     Verify 200
//     Verify response body has more than 1 count of the id.
//     Verify response body has a Mixer span.
//     Retry if verification failed, up to 10 times per trace being tested.

// What tests we should have:
// 1) Verify Mixer adds trace spans on Check calls to Mixer.
// 2) Verify Pilot configures tracing to zipkin.

// To replicate:
// Have a and b apps, plus pilot, plus zipkin.
//

// To opt-in to the test framework, implement a TestMain, and call test.Run.
func TestMain(m *testing.M) {
	framework.Run("zipkin_test", m)
}
