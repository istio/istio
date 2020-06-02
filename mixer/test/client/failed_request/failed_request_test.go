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

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/test/client/env"
)

const (
	mixerAuthFailMessage = "Unauthenticated by mixer."
)

// Check attributes from a fail GET request from mixer
const checkAttributesMixerFail = `
{
  "context.protocol": "http",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "destination.uid": "",
  "destination.namespace": "",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  }
}
`

// Report attributes from a fail GET request from mixer
const reportAttributesMixerFail = `
{
  "check.error_code": 16,
  "check.error_message": "UNAUTHENTICATED:Unauthenticated by mixer.",
  "context.protocol": "http",
  "context.proxy_error_code": "UAEX",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 0,
  "response.time": "*",
  "response.size": 41,
  "response.duration": "*",
  "response.code": 401,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "41",
     ":status": "401",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 266
}
`

// Report attributes from a fail GET request from backend
const reportAttributesBackendFail = `
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
  "request.method": "GET",
  "request.scheme": "http",
  "request.url_path": "/echo",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
     ":method": "GET",
     ":path": "/echo",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 0,
  "response.time": "*",
  "response.size": 25,
  "response.duration": "*",
  "response.code": 400,
  "response.headers": {
     "date": "*",
     "content-length": "25",
     ":status": "400",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 289
}
`

func TestFailedRequest(t *testing.T) {
	s := env.NewTestSetup(env.FailedRequestTest, t)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "MixerFail"
	s.SetMixerCheckStatus(rpc.Status{
		Code:    int32(rpc.UNAUTHENTICATED),
		Message: mixerAuthFailMessage,
	})
	code, respBody, err := env.HTTPGet(url)
	// Make sure to restore r_status for next request.
	s.SetMixerCheckStatus(rpc.Status{})
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 401 {
		t.Errorf("Status code 401 is expected, got %d.", code)
	}
	if respBody != "UNAUTHENTICATED:"+mixerAuthFailMessage {
		t.Errorf("Error response body is not expected, got: '%s'.", respBody)
	}
	s.VerifyCheck(tag, checkAttributesMixerFail)
	s.VerifyReport(tag, reportAttributesMixerFail)

	// Issues a failed request caused by backend
	tag = "BackendFail"
	headers := map[string]string{}
	headers[env.FailHeader] = "Yes"
	code, respBody, err = env.HTTPGetWithHeaders(url, headers)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 400 {
		t.Errorf("Status code 400 is expected, got %d.", code)
	}
	if respBody != env.FailBody {
		t.Errorf("Error response body is not expected, got '%s'.", respBody)
	}
	// Same Check attributes as the first one.
	s.VerifyCheck(tag, checkAttributesMixerFail)
	s.VerifyReport(tag, reportAttributesBackendFail)
}
