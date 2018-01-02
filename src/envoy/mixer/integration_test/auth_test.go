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

	mccpb "istio.io/api/mixer/v1/config/client"
)

const (
	JWT_ISSUER  = "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com"
	JWT_CLUSTER = "service1"
)

//
// How to generate a long live token:
// * git clone https://github.com/cloudendpoints/esp
// * modify file: bazel-esp/external/grpc_git/src/core/lib/security/credentials/jwt/json_token.c
//     remove the lines using grpc_max_auth_token_lifetime()
// * modify src/api_manager/auth/lib/auth_token.cc
//     change TOKEN_LIFETIME = 3600000000
// * bazel build //src/tools:all
// * client/custom/gen-auth-token.sh -s src/nginx/t/matching-client-secret.json
//
const longLiveToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6ImIzMzE5YTE0NzUxNGRmN2VlNWU0YmNkZWU1MTM1MGNjODkwY2M4OWUifQ==.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJib29rc3RvcmUtZXNwLWVjaG8uY2xvdWRlbmRwb2ludHNhcGlzLmNvbSIsImlhdCI6MTUxMjc1NDIwNSwiZXhwIjo1MTEyNzU0MjA1fQ==.HKWpc8zLw7NAzlgPphHpQ6fWh7k1cJ0XM7B_9YqcOQYLe8UA9KvOC_4D6cNw7HCaEv8UQufA4d8ErDn5PI3mPxn6m8pciJbcqblXmNN8jCJUSH2OHZsWDdzipHPrt5kxz9onx39m9Zdb_xXAffHREVDXO6eMzNte8ZihZwmZauIT9fbL8BbD74_D5tQvswdjUNAQuTdK6-pBXOH1Qf7fE3V92ESVqUmqM05FkTBfDZw6CGKj47W8ecs0QiLyERth8opCTLsRi5QN1xEPggTpfH_YBZTtsuIybVjiw9UAizWE-ziFWx2qlt9JPEArjvroMfNmJz4gTenbKNuXBMJOQg=="

const checkAttributes = `
{
  "context.protocol": "http",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "mesh3.ip": "[0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 8]",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "source.ip": "[127 0 0 1]",
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
  "request.auth.audiences": "bookstore-esp-echo.cloudendpointsapis.com",
  "request.auth.principal": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com/628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
  "request.auth.claims": {
     "iss": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
     "aud": "bookstore-esp-echo.cloudendpointsapis.com",
     "sub": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com"
  }
}
`

const reportAttributes = `
{
  "context.protocol": "http",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "mesh3.ip": "[0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 8]",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "localhost:27070",
     "sec-istio-auth-userinfo": "eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJib29rc3RvcmUtZXNwLWVjaG8uY2xvdWRlbmRwb2ludHNhcGlzLmNvbSIsImlhdCI6MTUxMjc1NDIwNSwiZXhwIjo1MTEyNzU0MjA1fQ==",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.auth.audiences": "bookstore-esp-echo.cloudendpointsapis.com",
  "request.auth.principal": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com/628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
  "request.auth.claims": {
     "iss": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
     "aud": "bookstore-esp-echo.cloudendpointsapis.com",
     "sub": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com"
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

const FailedReportAttributes = `
{
  "context.protocol": "http",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "mesh3.ip": "[0 1 0 0 0 0 0 0 0 0 0 0 0 0 0 8]",
  "request.host": "localhost:27070",
  "request.path": "/echo",
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
  "source.uid": "POD11",
  "source.namespace": "XYZ11",
  "target.name": "target-name",
  "target.user": "target-user",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.headers": {
     ":method": "GET",
     ":path": "/echo",
     ":authority": "localhost:27070",
     "x-forwarded-proto": "http",
     "authorization": "Bearer invalidtoken",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 0,
  "response.time": "*",
  "response.size": 14,
  "response.duration": "*",
  "response.code": 401,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "14",
     ":status": "401",
     "server": "envoy"
  }
}
`

func TestJWTAuth(t *testing.T) {
	s := &TestSetup{
		t:  t,
		v2: GetDefaultV2Conf(),
	}

	// pubkey server is the same as backend server.
	// Empty audiences.
	AddJwtAuth(s.v2.HttpServerConf, &mccpb.JWT{
		Issuer:              JWT_ISSUER,
		JwksUri:             fmt.Sprintf("http://localhost:%d/pubkey", BackendPort),
		JwksUriEnvoyCluster: JWT_CLUSTER,
	})
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	tag := "Good-Token"
	headers := map[string]string{}
	headers["Authorization"] = fmt.Sprintf("Bearer %v", longLiveToken)
	code, _, err := HTTPGetWithHeaders(url, headers)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}
	s.VerifyCheck(tag, checkAttributes)
	s.VerifyReport(tag, reportAttributes)

	tag = "Invalid-Token"
	headers["Authorization"] = fmt.Sprintf("Bearer invalidtoken")
	code, _, err = HTTPGetWithHeaders(url, headers)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 401 {
		t.Errorf("Status code 401 is expected, got %d.", code)
	}
	s.VerifyReport(tag, FailedReportAttributes)
}
