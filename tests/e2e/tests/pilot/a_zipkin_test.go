// Copyright 2017 Istio Authors
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

// TODO(nmittler): Named file with prefix "a_" to force golang to run it first.  Running it last results in flakes.

package pilot

import (
	"fmt"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"
)

const (
	traceHeader         = "X-Client-Trace-Id"
	numTraces           = 5
	mixerCheckOperation = "mixer/check"
	traceIDField        = "\"traceId\""
)

func TestZipkin(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/7695")
	for i := 0; i < numTraces; i++ {
		testName := fmt.Sprintf("index_%d", i)
		traceSent := false
		var id string

		runRetriableTest(t, testName, defaultRetryBudget, func() error {
			if !traceSent {
				// Send a request with a trace header.
				id = uuid.NewV4().String()
				response := ClientRequest(primaryCluster, "a", "http://b", 1,
					fmt.Sprintf("--key %v --val %v", traceHeader, id))
				if !response.IsHTTPOk() {
					// Keep retrying until we successfully send a trace request.
					return errAgain
				}

				traceSent = true
			}

			// Check the zipkin server to verify the trace was received.
			response := ClientRequest(
				primaryCluster,
				"t",
				fmt.Sprintf("http://zipkin.%s:9411/api/v1/traces?annotationQuery=guid:x-client-trace-id=%s",
					tc.Kube.IstioSystemNamespace(), id),
				1, "",
			)

			if !response.IsHTTPOk() {
				return errAgain
			}

			// Check that the trace contains the id value (must occur more than once, as the
			// response also contains the request URL with query parameter).
			if strings.Count(response.Body, id) == 1 {
				return errAgain
			}

			// If first invocation (due to mixer check result caching), then check that the mixer
			// span is also included in the trace
			// a) Count the number of spans - should be 2, one for the invocation of service b, and the other for the
			//		server span associated with the mixer check
			// b) Check that the trace data contains the mixer/check (part of the operation name for the server span)
			// NOTE: We are also indirectly verifying that the mixer/check span is a child span of the service invocation, as
			// the mixer/check span can only exist in this trace as a child span. If it wasn't a child span then it would be
			// in a separate trace instance not retrieved by the query based on the single x-client-trace-id.
			if i == 0 && (strings.Count(response.Body, traceIDField) != 2 ||
				!strings.Contains(response.Body, mixerCheckOperation)) {
				return errAgain
			}

			return nil
		})
	}
}
