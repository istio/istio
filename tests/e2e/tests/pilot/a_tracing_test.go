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
	mixerCheckOperation = "\"name\":\"/istio.mixer.v1.mixer/check\""
	checkOperation      = "\"name\":\"check\""
	traceIDField        = "\"traceId\""
)

func TestTracingMixer(t *testing.T) {
	traceSent := false
	var id string

	runRetriableTest(t, primaryCluster, "isMixerTraced", defaultRetryBudget, func() error {
		if !traceSent {
			// Send a request with a trace header.
			id = uuid.NewV4().String()
			response := ClientRequest(primaryCluster, "a", "http://b", 1,
				fmt.Sprintf("-key %v -val %v", traceHeader, id))
			if !response.IsHTTPOk() {
				// Keep retrying until we successfully send a trace request.
				return errAgain
			}

			traceSent = true
		}

		// Check the tracing server to verify the trace was received.
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

		// Check that the mixer spans are also included in the trace:
		// a) Check that the trace data contains the "check" operation name associated with the mixer check client span
		// b) Check that the trace data contains the "/istio.mixer.v1.mixer/check" operation name for the server span
		// c) Count the number of spans - should be 4, one for the invocation of service b, and the other 3 for the
		//		mixer check
		// NOTE: We are also indirectly verifying that the mixer check spans are child spans of the service invocation, as
		// the mixer check spans can only exist in this trace as child spans. If they weren't child spans then they would be
		// in a separate trace instance not retrieved by the query based on the single x-client-trace-id.
		if !strings.Contains(response.Body, checkOperation) {
			return fmt.Errorf("check operation (%s) not found in response body: %s", checkOperation, response.Body)
		}
		if !strings.Contains(response.Body, mixerCheckOperation) {
			return fmt.Errorf("mixer Check operation (%s) not found in response body: %s", mixerCheckOperation, response.Body)
		}
		if spanCount := strings.Count(response.Body, traceIDField); spanCount != 4 {
			return fmt.Errorf("number of spans for trace (expected 4) is: %d", spanCount)
		}

		return nil
	})
}

func TestTracingEnabled(t *testing.T) {
	traceSent := false
	var id string

	runRetriableTest(t, primaryCluster, "checkEnabled", defaultRetryBudget, func() error {
		if !traceSent {
			// Send a request with a trace header.
			id = uuid.NewV4().String()
			response := ClientRequest(primaryCluster, "a", "http://b", 1,
				fmt.Sprintf("-key %v -val %v", traceHeader, id))
			if !response.IsHTTPOk() {
				// Keep retrying until we successfully send a trace request.
				return errAgain
			}

			traceSent = true
		}

		// Check the tracing server to verify the trace was received.
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
		if idCount := strings.Count(response.Body, id); idCount <= 1 {
			return fmt.Errorf("id should occur more than once: %d", idCount)
		}

		return nil
	})
}
