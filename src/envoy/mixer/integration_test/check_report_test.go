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
)

// Check attributes from a good GET request
const checkAttributesOkGet = `
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

// Report attributes from a good GET request
const reportAttributesOkGet = `
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
  "response.size": 0,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain; charset=utf-8",
     "content-length": "0",
     ":status": "200",
     "server": "envoy"
  }
}
`

// Check attributes from a good POST request
const checkAttributesOkPost = `
{
  "context.protocol": "http",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
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
     ":method": "POST",
     ":path": "/echo",
     ":authority": "localhost:27070",
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
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
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
     ":method": "POST",
     ":path": "/echo",
     ":authority": "localhost:27070",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 12,
  "response.time": "*",
  "response.size": 12,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "12",
     ":status": "200",
     "server": "envoy"
  }
}
`

func TestCheckReportAttributes(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkGet)
	s.VerifyReport(tag, reportAttributesOkGet)

	// Issues a POST request.
	tag = "OKPost"
	if _, _, err := HTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesOkPost)
}
