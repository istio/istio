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

package main

import (
	"fmt"
	"strings"
	"sync"

	uuid "github.com/satori/go.uuid"
)

const (
	traceHeader = "X-Client-Trace-Id"
	numTraces   = 5
)

type zipkin struct {
	*infra
	mutex  sync.Mutex
	traces []string
}

func (t *zipkin) String() string {
	return "zipkin"
}

func (t *zipkin) setup() error {
	if !t.Zipkin {
		return nil
	}

	t.traces = make([]string, 0, numTraces)
	return nil
}

// ensure that requests are picked up by Zipkin
func (t *zipkin) run() error {
	if !t.Zipkin {
		return nil
	}

	if err := t.makeRequests(); err != nil {
		return err
	}

	return t.verifyTraces()
}

// make requests for Zipkin to pick up
func (t *zipkin) makeRequests() error {
	funcs := make(map[string]func() status)
	for i := 0; i < numTraces; i++ {
		funcs[fmt.Sprintf("Zipkin trace request %d", i)] = func() status {
			id := uuid.NewV4()
			response := t.infra.clientRequest("a", "http://b", 1,
				fmt.Sprintf("-key %v -val %v", traceHeader, id))
			if len(response.code) > 0 && response.code[0] == httpOk {
				t.mutex.Lock()
				t.traces = append(t.traces, id.String())
				t.mutex.Unlock()
				return nil
			}
			return errAgain
		}
	}
	return parallel(funcs)
}

// verify that the traces were picked up by Zipkin
func (t *zipkin) verifyTraces() error {
	f := func() status {
		for _, id := range t.traces {
			response := t.infra.clientRequest(
				"t",
				fmt.Sprintf("http://zipkin:9411/api/v1/traces?annotationQuery=guid:x-request-id=/%s", id),
				1, "",
			)

			if len(response.code) == 0 || response.code[0] != httpOk {
				return errAgain
			}

			// ensure that sent trace IDs are a subset of the trace IDs in Zipkin.
			// this is inefficient, but the alternatives are ugly regexps or extensive JSON parsing.
			if !strings.Contains(response.body, id) {
				return errAgain
			}
		}
		return nil
	}

	return parallel(map[string]func() status{
		"Ensure traces are picked up by Zipkin": f,
	})
}

func (t *zipkin) teardown() {
}
