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

package test

import (
	"fmt"
	"testing"

	rpc "github.com/googleapis/googleapis/google/rpc"
)

const (
	mixerAuthFailMessage  = "Unauthenticated by mixer."
	mixerQuotaFailMessage = "Not enough quota by mixer."
)

// Attributes verification rules
// 1) If value is *,  key must exist, but value is not checked.
// 1) If value is -,  key must NOT exist.
// 3) At top level attributes, not inside StringMap, all keys must
//    be listed. Extra keys are NOT allowed
// 3) Inside StringMap, not need to list all keys. Extra keys are allowed
//
// Attributes provided from envoy config
// * source.id and source.namespace are forwarded from client proxy
// * target.id and target.namespace are from server proxy
//
// HTTP header "x-istio-attributes" is used to forward attributes between
// proxy. It should be removed before calling mixer and backend.
//
// Check attributes from a good GET request
const checkAttributesOkGet = `
{
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "response.latency": "*",
  "response.http.code": 200,
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
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "response.size": 45,
  "response.latency": "*",
  "response.http.code": 429,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "45",
     ":status": "429",
     "server": "envoy"
  }
}
`

// Check attributes from a fail GET request from mixer
const checkAttributesMixerFail = `
{
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "response.latency": "*",
  "response.http.code": 401,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "41",
     ":status": "401",
     "server": "envoy"
  }
}
`

// Check attributes from a fail GET request from backend
const checkAttributesBackendFail = `
{
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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

// Report attributes from a fail GET request from backend
const reportAttributesBackendFail = `
{
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
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
  "response.latency": "*",
  "response.http.code": 400,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain; charset=utf-8",
     "content-length": "25",
     ":status": "400",
     "server": "envoy"
  }
}
`

func verifyAttributes(
	s *TestSetup, tag string, check string, report string, t *testing.T) {
	_ = <-s.mixer.check.ch
	if err := Verify(s.mixer.check.bag, check); err != nil {
		t.Fatalf("Failed to verify %s check: %v\n, Attributes: %+v",
			tag, err, s.mixer.check.bag)
	}

	_ = <-s.mixer.report.ch
	if err := Verify(s.mixer.report.bag, report); err != nil {
		t.Fatalf("Failed to verify %s report: %v\n, Attributes: %+v",
			tag, err, s.mixer.report.bag)
	}
}

func verifyQuota(s *TestSetup, tag string, t *testing.T) {
	_ = <-s.mixer.quota.ch
	if s.mixer.quota_request.Quota != "RequestCount" {
		t.Fatalf("Failed to verify %s quota name (=RequestCount): %v\n",
			tag, s.mixer.quota_request.Quota)
	}
	if s.mixer.quota_request.Amount != 5 {
		t.Fatalf("Failed to verify %s quota amount (=5): %v\n",
			tag, s.mixer.quota_request.Amount)
	}
}

func TestMixer(t *testing.T) {
	s, err := SetUp()
	if err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// There is a client proxy with filter "forward_attribute"
	// and a server proxy with filter "mixer" calling mixer.
	// This request will connect to client proxy, to server proxy
	// and to the backend.
	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	// Issues a GET echo request with 0 size body
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in GET request: %v", err)
	}
	verifyAttributes(&s, "OkGet",
		checkAttributesOkGet, reportAttributesOkGet, t)
	verifyQuota(&s, "OkGet", t)

	// Issues a failed POST request caused by Mixer Quota
	s.mixer.quota.r_status = rpc.Status{
		Code:    int32(rpc.RESOURCE_EXHAUSTED),
		Message: mixerQuotaFailMessage,
	}
	code, resp_body, err := HTTPPost(url, "text/plain", "Hello World!")
	// Make sure to restore r_status for next request.
	s.mixer.quota.r_status = rpc.Status{}
	if err != nil {
		t.Errorf("Failed in POST request: %v", err)
	}
	if code != 429 {
		t.Errorf("Status code 429 is expected.")
	}
	if resp_body != "RESOURCE_EXHAUSTED:"+mixerQuotaFailMessage {
		t.Errorf("Error response body is not expected.")
	}
	verifyAttributes(&s, "OkPost",
		checkAttributesOkPost, reportAttributesOkPost, t)
	verifyQuota(&s, "OkPost", t)

	// Issues a failed request caused by mixer
	s.mixer.check.r_status = rpc.Status{
		Code:    int32(rpc.UNAUTHENTICATED),
		Message: mixerAuthFailMessage,
	}
	code, resp_body, err = HTTPGet(url)
	// Make sure to restore r_status for next request.
	s.mixer.check.r_status = rpc.Status{}
	if err != nil {
		t.Errorf("Failed in GET request: error: %v", err)
	}
	if code != 401 {
		t.Errorf("Status code 401 is expected.")
	}
	if resp_body != "UNAUTHENTICATED:"+mixerAuthFailMessage {
		t.Errorf("Error response body is not expected.")
	}
	verifyAttributes(&s, "MixerFail",
		checkAttributesMixerFail, reportAttributesMixerFail, t)
	// Not quota call due to Mixer failure.

	// Issues a failed request caused by backend
	headers := map[string]string{}
	headers[FailHeader] = "Yes"
	code, resp_body, err = HTTPGetWithHeaders(url, headers)
	if err != nil {
		t.Errorf("Failed in GET request: error: %v", err)
	}
	if code != 400 {
		t.Errorf("Status code 400 is expected.")
	}
	if resp_body != FailBody {
		t.Errorf("Error response body is not expected.")
	}
	verifyAttributes(&s, "BackendFail",
		checkAttributesBackendFail, reportAttributesBackendFail, t)
	verifyQuota(&s, "BackendFail", t)
}
