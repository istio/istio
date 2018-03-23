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

package pilot

import (
	"fmt"
	"strings"
	"sync"

	uuid "github.com/satori/go.uuid"

	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

const (
	traceHeader = "X-Client-Trace-Id"
	numTraces   = 5
	traceIdField = "\"traceId\""
	mixerCheckOperation = "mixer/check"
)

type zipkin struct {
	*tutil.Environment
	mutex  sync.Mutex
	traces []string
}

func (t *zipkin) String() string {
	return "zipkin"
}

func (t *zipkin) Setup() error {
	if !t.Config.Zipkin {
		return nil
	}

	t.traces = make([]string, 0, numTraces)
	return nil
}

// ensure that requests are picked up by Zipkin
func (t *zipkin) Run() error {
	if !t.Config.Zipkin {
		return nil
	}

	if err := t.makeRequests(); err != nil {
		return err
	}

	return t.verifyTraces()
}

// make requests for Zipkin to pick up
func (t *zipkin) makeRequests() error {
	funcs := make(map[string]func() tutil.Status)
	for i := 0; i < numTraces; i++ {
		funcs[fmt.Sprintf("Zipkin trace request %d", i)] = func() tutil.Status {
			id := uuid.NewV4()
			response := t.Environment.ClientRequest("a", "http://b", 1,
				fmt.Sprintf("-key %v -val %v", traceHeader, id))
			if response.IsHTTPOk() {
				t.mutex.Lock()
				t.traces = append(t.traces, id.String())
				t.mutex.Unlock()
				return nil
			}
			return tutil.ErrAgain
		}
	}
	return tutil.Parallel(funcs)
}

// verify that the traces were picked up by Zipkin
func (t *zipkin) verifyTraces() error {
	f := func() tutil.Status {
		for _, id := range t.traces {
			response := t.Environment.ClientRequest(
				"t",
				fmt.Sprintf("http://zipkin.%s:9411/api/v1/traces?annotationQuery=guid:x-client-trace-id=%s",
					t.Config.IstioNamespace, id),
				1, "",
			)

			if !response.IsHTTPOk() {
				return tutil.ErrAgain
			}

			// Check that:
			// a) The trace contains the id value (must occur more than once, as the response also contains the request URL with query parameter)
			// b) Count the number of spans - should be 2, one for the invocation of service b, and the other for the mixer check
			// c) Check that the trace data contains the mixer/check (part of the operation name)
			// NOTE: We are also indirectly verifying that the mixer/check span is a child span of the service invocation, as
			// the mixer/check span can only exist in this trace as a child span. If it wasn't a child span then it would be
			// in a separate trace instance not retrieved by the query to zipkin (based on the single x-client-trace-id).

			if strings.Count(response.Body, id) == 1 || strings.Count(response.Body, traceIdField) != 2 || !strings.Contains(response.Body, mixerCheckOperation) {
				return tutil.ErrAgain
			}
		}
		return nil
	}

	return tutil.Parallel(map[string]func() tutil.Status{
		"Ensure traces are picked up by Zipkin": f,
	})
}

func (t *zipkin) Teardown() {
}
