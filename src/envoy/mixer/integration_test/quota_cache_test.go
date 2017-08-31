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
	mixerpb "istio.io/api/mixer/v1"
	"testing"
)

func TestQuotaCache(t *testing.T) {
	// Only check cache is enabled, quota cache is enabled.
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + quotaCacheConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

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
	s.mixer.check_referenced = &output

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	s.mixer.quota_limit = 10
	reject := 0
	ok := 0
	for i := 0; i < 20; i++ {
		code, _, err := HTTPGet(url)
		if err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
		if code == 200 {
			ok++
		} else if code == 429 {
			reject++
		}
	}
	if ok != 10 || reject != 10 {
		t.Fatalf("Unexpected quota ok count %v, reject count %v", ok, reject)
	}
	// Less than 5 time of Quota is called.
	if s.mixer.quota.count >= 5 {
		t.Fatalf("%s quota called count %v should not be more than 5",
			tag, s.mixer.quota.count)
	}
}
