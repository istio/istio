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

// Check attributes from a good POST request
const checkAttributesOkPost = `
{
  "context.protocol": "http",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "destination.uid": "",
  "destination.namespace": "",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "request.headers": {
     ":method": "POST",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  }
}
`

// Report attributes from a good POST request
const reportAttributesOkPost = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "destination.uid": "",
  "destination.namespace": "",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "check.cache_hit": false,
  "quota.cache_hit": false,
  "request.headers": {
     ":method": "POST",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": "10485760",
  "response.time": "*",
  "response.size": "10485760",
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "10485760",
     ":status": "200",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": "*"
}
`

func TestCheckReportLargePostRequest(t *testing.T) {
	s := env.NewTestSetup(env.CheckReportLargePostRequestTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// Issues a POST request with 10 MB request body. This request is sent to ServerProxyPort
	// directly. This verifies that the Mixer filter at ingress Envoy could handle large request.
	// TODO(JimmyCYJ): use ClientProxyPort after issuer envoyproxy/envoy#2929 is fixed.
	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)
	tag := "OKPost"
	byteArray := make([]byte, 10*1024*1024)
	for i := range byteArray {
		byteArray[i] = 'x'
	}
	reqBody := string(byteArray)
	code, _, err := env.HTTPPost(url, "text/plain", reqBody)
	if code != 200 {
		t.Errorf("Expect 200 OK but receive %s: %d", tag, code)
	} else if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesOkPost)
}
