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

package client_test

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

// Report attributes from a GET request for Rbac global permissive mode.
const reportAttributes = `
{
	"check.cache_hit": false,
	"connection.mtls": false,
	"context.protocol": "http",
	"context.proxy_error_code": "*",
	"destination.ip": "[127 0 0 1]",
	"destination.port": "*",
	"mesh1.ip": "*",
	"mesh2.ip": "*",
	"mesh3.ip": "*",
	"origin.ip": "[127 0 0 1]",
	"quota.cache_hit": false,
	"request.headers": {
		":method": "GET",
		":path": "/echo",
		":authority": "*",
		"x-forwarded-proto": "http",
		"x-istio-attributes": "-",
		"x-request-id": "*"
	},
	"request.host": "*",
	"request.method": "GET",
	"request.path": "/echo",
	"request.scheme": "http",
	"request.size": 0,
	"request.time": "*",
	"request.total_size": "*",
	"request.url_path": "/echo",
	"request.useragent": "Go-http-client/1.1",
	"response.code": 200,
	"response.duration": "*",
	"response.headers": {
		"date": "*",
		"content-length": "0",
		":status": "200",
		"server": "envoy"
	},
	"response.size": 0,
	"response.time": "*",
	"response.total_size": "*",
	"source.namespace": "XYZ11",
	"source.uid": "POD11",
	"target.name": "target-name",
	"target.namespace": "XYZ222",
	"target.uid": "POD222",
	"target.user": "target-user",
	"rbac.permissive.response_code": "403"
}
`

const rbacGlobalPermissiveFilter = `
- name: envoy.filters.http.rbac
  config:
    shadow_rules:
      policies: {}
`

func TestRbacGlobalPermissive(t *testing.T) {
	s := env.NewTestSetup(env.RbacGlobalPermissiveTest, t)
	s.SetFiltersBeforeMixer(rbacGlobalPermissiveFilter)
	// Rbac filer is before mixer filter.
	env.SetDefaultServiceConfigMap(s.MfConfig())

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "RbacGlobalPermissive"
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}

	s.VerifyReport(tag, reportAttributes)
}
