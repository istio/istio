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

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
)

const (
	mixerQuotaFailMessage = "Quota is exhausted for: RequestCount"
)

func TestQuotaCall(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + quotaConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet v1"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyQuota(tag, "RequestCount", 5)

	// Issues a failed POST request caused by Mixer Quota
	tag = "QuotaFail v1"
	s.mixer.quota.r_status = rpc.Status{
		Code:    int32(rpc.RESOURCE_EXHAUSTED),
		Message: mixerQuotaFailMessage,
	}
	code, resp_body, err := HTTPPost(url, "text/plain", "Hello World!")
	// Make sure to restore r_status for next request.
	s.mixer.quota.r_status = rpc.Status{}
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 429 {
		t.Errorf("Status code 429 is expected.")
	}
	if resp_body != "RESOURCE_EXHAUSTED:"+mixerQuotaFailMessage {
		t.Errorf("Error response body is not expected.")
	}
	s.VerifyQuota(tag, "RequestCount", 5)

	//
	// Use V2 config
	//

	s.v2 = GetDefaultV2Conf()
	// Add v2 quota config for all requests.
	AddHttpQuota(s.v2.HttpServerConf, "RequestCount", 5)
	s.ReStartEnvoy()

	// Issues a GET echo request with 0 size body
	tag = "OKGet"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyQuota(tag, "RequestCount", 5)
}
