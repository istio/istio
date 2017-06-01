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
	okRequestNum = 10
	// Pool may have some prefetched tokens.
	// In order to see rejected request, reject request num > 20
	// the minPrefetch * 2.
	rejectRequestNum = 30
)

// testQuotaCache has been disabled
// Quota call also needs all the attributes
// that Check needs. Therefore a key formed by using all
// attributes is very unlikely to hit the cache.
func testQuotaCache(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + quotaCacheConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// drain all channel.
	s.DrainMixerAllChannels()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	ok := 0
	// Will trigger a new prefetch after half of minPrefech is used.
	for i := 0; i < okRequestNum; i++ {
		code, _, err := HTTPGet(url)
		if err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
		if code == 200 {
			ok++
		}
	}
	// Two quota calls are triggered for 10 requests:
	// minPrefetch is 10, a new prefetch is started at 1/2 of minPrefetch.
	// 1) prefetch amount = 10
	// 2) prefetch amount = 10  after 5 requests.
	if s.mixer.quota.count > 2 {
		s.t.Fatalf("%s mixer quota call count: %v, should not be more than 2",
			tag, s.mixer.quota.count)
	}
	if ok < okRequestNum {
		s.t.Fatalf("%s granted request count: %v, should be %v",
			tag, ok, okRequestNum)
	}

	// Reject the quota call from Mixer.
	tag = "QuotaFail"
	s.mixer.quota.r_status = rpc.Status{
		Code:    int32(rpc.RESOURCE_EXHAUSTED),
		Message: "Not enought qouta.",
	}
	reject := 0
	others := 0
	for i := 0; i < rejectRequestNum; i++ {
		code, _, err := HTTPGet(url)
		if err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
		if code == 200 {
			ok++
		} else if code == 429 {
			reject++
		} else {
			others++
		}
	}
	// Total should be 3 qutoa calls.
	// minPrefetch is 10, a new prefetch is started at 1/2 of minPrefetch.
	// 1) prefetch amount = 10  granted 10
	// 2) prefetch amount = 10  after 5 requests.  granted 10
	// 3) prefetch amount = 14  after 14 requests,  rejected.
	if s.mixer.quota.count > 3 {
		s.t.Fatalf("%s mixer quota call count: %v, should not be more than 3",
			tag, s.mixer.quota.count)
	}
	// Should be more than 20 requests rejected.
	// Mixer server granted 20 tokens (from two prefetch calls).
	if reject < 20 {
		s.t.Fatalf("%s rejected request count: %v should be equal to 20.",
			tag, reject)
	}
	if others > 0 {
		s.t.Fatalf("%s should not have any other failures: %v", tag, others)
	}
}
