// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"fmt"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"
)

const (
	mixerAuthFailMessage = "Unauthenticated by mixer."
)

// Check attributes from a fail GET request from mixer
const checkAttributesMixerFail = `
{
  "context.protocol": "http",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "source.name": "source-name",
  "source.user": "source-user",
  "source.ip": "*",
  "source.port": "*",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "localhost:27070",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  }
}
`

// Report attributes from a fail GET request from mixer
const reportAttributesMixerFail = `
{
  "context.protocol": "http",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "source.name": "source-name",
  "source.user": "source-user",
  "source.ip": "*",
  "source.port": "*",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "localhost:27070",
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
  }
}
`

// Report attributes from a fail GET request from backend
const reportAttributesBackendFail = `
{
  "context.protocol": "http",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "source.name": "source-name",
  "source.user": "source-user",
  "source.ip": "*",
  "source.port": "*",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "localhost:27070",
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
     "content-type": "text/plain; charset=utf-8",
     "content-length": "25",
     ":status": "400",
     "server": "envoy"
  }
}
`

func TestFailedRequest(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	tag := "MixerFail"
	s.mixer.check.r_status = rpc.Status{
		Code:    int32(rpc.UNAUTHENTICATED),
		Message: mixerAuthFailMessage,
	}
	code, resp_body, err := HTTPGet(url)
	// Make sure to restore r_status for next request.
	s.mixer.check.r_status = rpc.Status{}
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 401 {
		t.Errorf("Status code 401 is expected, got %d.", code)
	}
	if resp_body != "UNAUTHENTICATED:"+mixerAuthFailMessage {
		t.Errorf("Error response body is not expected, got: '%s'.", resp_body)
	}
	s.VerifyCheck(tag, checkAttributesMixerFail)
	s.VerifyReport(tag, reportAttributesMixerFail)

	// Issues a failed request caused by backend
	tag = "BackendFail"
	headers := map[string]string{}
	headers[FailHeader] = "Yes"
	code, resp_body, err = HTTPGetWithHeaders(url, headers)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 400 {
		t.Errorf("Status code 400 is expected, got %d.", code)
	}
	if resp_body != FailBody {
		t.Errorf("Error response body is not expected, got '%s'.", resp_body)
	}
	// Same Check attributes as the first one.
	s.VerifyCheck(tag, checkAttributesMixerFail)
	s.VerifyReport(tag, reportAttributesBackendFail)
}
