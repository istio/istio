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

package client_test

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

// Report attributes from a fault inject GET request
const reportAttributes = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "FI",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "origin.ip": "[127 0 0 1]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "destination.uid": "",
  "destination.namespace": "",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 0,
  "response.time": "*",
  "response.size": 18,
  "response.duration": "*",
  "response.code": 503,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "18",
     ":status": "503",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 0,
  "request.url_path": "/echo"
}
`

const allAbortFaultFilter = `
- name: envoy.fault
  config:
    abort:
      percentage:
        numerator: 100
        denominator: HUNDRED
      http_status: 503
`

func TestFaultInject(t *testing.T) {
	s := env.NewTestSetup(env.FaultInjectTest, t)
	s.SetFiltersBeforeMixer(allAbortFaultFilter)
	// fault injection filer is before mixer filter.
	// If a request is rejected, per-route service config could not
	// be used, has to use default service_configs map to send Report.
	env.SetDefaultServiceConfigMap(s.MfConfig())

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "FaultInject"
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 503 {
		t.Errorf("Status code 503 is expected, got %d.", code)
	}
	// Fault filter is before Mixer, Check is not called.
	s.VerifyReport(tag, reportAttributes)
}
