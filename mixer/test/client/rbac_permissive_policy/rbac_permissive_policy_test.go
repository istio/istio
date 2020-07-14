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

// Report attributes from a GET request for Rbac permissive mode at policy level.
const reportAttributes = `
{
  "connection.mtls": false,
  "context.protocol": "http",
  "context.proxy_error_code": "*",
  "context.reporter.uid" : "",
  "destination.namespace" : "",
  "destination.uid": "",
  "envoy.filters.http.rbac": {
    "shadow_effective_policy_id": "details-reviews-viewer",
    "shadow_engine_result": "allowed"
  },
  "rbac.permissive.effective_policy_id": "details-reviews-viewer",
  "rbac.permissive.response_code" : "allowed",
  "mesh1.ip": "*",
  "mesh2.ip": "*",
  "origin.ip": "[127 0 0 1]",
  "request.headers": {
      ":method": "GET",
      ":path": "/echo",
      ":authority": "*",
      "x-forwarded-proto": "http",
      "x-request-id": "*",
      "user-agent": "Go-http-client/1.1",
      "content-length": "*",
      "accept-encoding":"gzip"
  },
  "request.host": "*",
  "request.method": "GET",
  "request.path": "/echo",
  "request.scheme": "http",
  "request.size": 0,
  "request.time": "*",
  "request.total_size": 0,
  "request.url_path": "/echo",
  "request.useragent": "Go-http-client/1.1",
  "response.code": 403,
  "response.duration": "*",
  "response.headers": {
      "date": "*",
      "content-length": "*",
      ":status": "403",
      "server": "envoy",
      "content-type":"text/plain"
  },
  "response.size": "*",
  "response.time": "*",
  "response.total_size": "*",
  "source.namespace": "XYZ11",
  "source.uid": "POD11",
  "target.name": "target-name",
  "target.namespace": "XYZ222",
  "target.uid": "POD222",
  "target.user": "target-user"
}
`

const rbacPolicyPermissiveFilter = `
- name: envoy.filters.http.rbac
  config:
    rules:
      policies: {}
    shadow_rules:
      policies:
        details-reviews-viewer:
          permissions:
          - and_rules:
              rules:
              - or_rules:
                  rules:
                  - header:
                      name: ":method"
                      exact_match: GET
          principals:
          - any: true
`

// Test senario of setting permissive mode on policy level.
// Response code is 403 since authorization is turned on and is deny-by-default.
// Permissive resp code is 200 because shadow rules is set.
func TestRbacPolicyPermissive(t *testing.T) {
	s := env.NewTestSetup(env.RbacPolicyPermissiveTest, t)
	s.SetFiltersBeforeMixer(rbacPolicyPermissiveFilter)
	// Rbac filer is before mixer filter.
	env.SetDefaultServiceConfigMap(s.MfConfig())

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "RbacPolicyPermissive"
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 403 {
		t.Errorf("Status code 403 is expected, got %d.", code)
	}

	s.VerifyReport(tag, reportAttributes)
}
