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

package showcase

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/dependency"
)

var cfg = ""

func TestHTTPWithMTLS(t *testing.T) {
	test.Requires(t, dependency.Apps, dependency.Pilot, dependency.MTLS)

	env := test.AcquireEnvironment(t)
	env.Configure(t, cfg)

	appa := env.GetAppOrFail("a", t)
	appt := env.GetAppOrFail("t", t)

	// Send requests to all of the HTTP endpoints.
	endpoints := appt.EndpointsForProtocol(model.ProtocolHTTP)
	for _, endpoint := range endpoints {
		url := endpoint.MakeURL()
		t.Run(url.String(), func(t *testing.T) {
			result := appa.CallOrFail(url, 1, nil, t)
			if !result.IsSuccess() {
				t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
			}
		})
	}
}
