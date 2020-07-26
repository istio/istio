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

// Report attributes from a good GET request
const reportAttributesOkGet = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo?a=b&c=d",
  "request.query_params": {"a": "b", "c": "d"},
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "GET",
  "request.scheme": "http",
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
     ":path": "/echo?a=b&c=d",
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
  "request.total_size": 274,
  "request.url_path": "/echo"
}
`

// Report attributes from a good POST request
const reportAttributesOkPost1 = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo?a=b&c=d",
  "request.query_params": {"a": "b", "c": "d"},
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
  "request.scheme": "http",
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
     ":method": "POST",
     ":path": "/echo?a=b&c=d",
     ":authority": "*",
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
  },
  "response.total_size": "*",
  "request.total_size": 310,
  "request.url_path": "/echo"
}
`

// Report attributes from a good POST request
const reportAttributesOkPost2 = `
{
  "context.protocol": "http",
  "context.proxy_error_code": "-",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "mesh2.ip": "[0 0 0 0 0 0 0 0 0 0 255 255 204 152 189 116]",
  "request.host": "*",
  "request.path": "/echo?a=b&c=d",
  "request.query_params": {"a": "b", "c": "d"},
  "request.time": "*",
  "request.useragent": "Go-http-client/1.1",
  "request.method": "POST",
  "request.scheme": "http",
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
     ":method": "POST",
     ":path": "/echo?a=b&c=d",
     ":authority": "*",
     "x-forwarded-proto": "http",
     "x-istio-attributes": "-",
     "x-request-id": "*"
  },
  "request.size": 18,
  "response.time": "*",
  "response.size": 18,
  "response.duration": "*",
  "response.code": 200,
  "response.headers": {
     "date": "*",
     "content-type": "text/plain",
     "content-length": "18",
     ":status": "200",
     "server": "envoy"
  },
  "response.total_size": "*",
  "request.total_size": 316,
  "request.url_path": "/echo"
}
`

// Stats in Envoy proxy.
var expectedStats = map[string]uint64{
	// Policy check stats
	"http_mixer_filter.total_check_calls":             3,
	"http_mixer_filter.total_check_cache_hits":        0,
	"http_mixer_filter.total_check_cache_misses":      3,
	"http_mixer_filter.total_check_cache_hit_accepts": 0,
	"http_mixer_filter.total_check_cache_hit_denies":  0,
	"http_mixer_filter.total_remote_check_calls":      3,
	"http_mixer_filter.total_remote_check_accepts":    3,
	"http_mixer_filter.total_remote_check_denies":     0,
	// Quota check stats
	"http_mixer_filter.total_quota_calls":                 0,
	"http_mixer_filter.total_quota_cache_hits":            0,
	"http_mixer_filter.total_quota_cache_misses":          0,
	"http_mixer_filter.total_quota_cache_hit_accepts":     0,
	"http_mixer_filter.total_quota_cache_hit_denies":      0,
	"http_mixer_filter.total_remote_quota_calls":          0,
	"http_mixer_filter.total_remote_quota_accepts":        0,
	"http_mixer_filter.total_remote_quota_denies":         0,
	"http_mixer_filter.total_remote_quota_prefetch_calls": 0,
	// Stats for RPCs to mixer policy server
	"http_mixer_filter.total_remote_calls":             3,
	"http_mixer_filter.total_remote_call_successes":    3,
	"http_mixer_filter.total_remote_call_timeouts":     0,
	"http_mixer_filter.total_remote_call_send_errors":  0,
	"http_mixer_filter.total_remote_call_other_errors": 0,
	// Report stats
	"http_mixer_filter.total_remote_report_calls": 1,
	"http_mixer_filter.total_report_calls":        3,
}

func TestReportBatch(t *testing.T) {
	s := env.NewTestSetup(env.ReportBatchTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo?a=b&c=d", s.Ports().ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Issues a POST request.
	tag = "OKPost1"
	if _, _, err := env.HTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Issues a POST request again.
	tag = "OKPost2"
	if _, _, err := env.HTTPPost(url, "text/plain", "Hello World Again!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	tag = "Batch"
	s.VerifyReport(tag, reportAttributesOkGet)
	s.VerifyReport(tag, reportAttributesOkPost1)
	s.VerifyReport(tag, reportAttributesOkPost2)

	// Check stats for Check, Quota and report calls.
	s.VerifyStats(expectedStats)
}
