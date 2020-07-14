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

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/test/client/env"
)

// Check attributes from a good GET request
const checkAttributesOkGet = `
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

// Report attributes from a good GET request.
// In reportAttributesOkGet[0], check.cache_hit and quota.cache_hit are false.
// In reportAttributesOkGet[1], check.cache_hit and quota.cache_hit are true.
var reportAttributesOkGet = [...]string{`{
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
  "response.size": 0,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
    "date": "*",
    "content-length": "0",
    ":status": "200",
    "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 266
}`,
	`{
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
  "check.cache_hit": true,
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
  "response.size": 0,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
    "date": "*",
    "content-length": "0",
    ":status": "200",
    "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 266
}`}

func TestCheckCacheHit(t *testing.T) {
	s := env.NewTestSetup(env.CheckCacheHitTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	// Need to override mixer test server Referenced field in the check response.
	// Its default is all fields in the request which could not be used fo test check cache.
	output := mixerpb.ReferencedAttributes{
		AttributeMatches: make([]mixerpb.ReferencedAttributes_AttributeMatch, 1),
	}
	output.AttributeMatches[0] = mixerpb.ReferencedAttributes_AttributeMatch{
		// Assume "target.name" is in the request attributes, and it is used for Check.
		Name:      10,
		Condition: mixerpb.EXACT,
	}
	s.SetMixerCheckReferenced(&output)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	for i := 0; i < 3; i++ {
		if _, _, err := env.HTTPGet(url); err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
		if i == 0 {
			s.VerifyCheck(tag, checkAttributesOkGet)
			s.VerifyReport(tag, reportAttributesOkGet[0])
		} else {
			s.VerifyReport(tag, reportAttributesOkGet[1])
		}
	}
	// Only the first check is called.
	s.VerifyCheckCount(tag, 1)
}
