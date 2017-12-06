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
	"log"
	"sync/atomic"
	"testing"
	"time"
)

const (
	concurrent         = 10
	duration_in_second = 15
)

func TestStressEnvoy(t *testing.T) {
	s := &TestSetup{
		t:      t,
		v2:     GetDefaultV2Conf(),
		stress: true,
	}
	// Not cache, enable quota
	AddHttpQuota(s.v2.HttpServerConf, "RequestCount", 1)
	DisableClientCache(s.v2.HttpServerConf, true, true, true)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	var count uint64 = 0
	for k := 0; k < concurrent; k++ {
		go func() {
			for true {
				if err := HTTPFastGet(url); err != nil {
					t.Errorf("Failed in request: %v", err)
				}
				atomic.AddUint64(&count, 1)
			}
		}()
	}
	time.Sleep(time.Second * time.Duration(duration_in_second))
	countFinal := atomic.LoadUint64(&count)
	log.Printf("Total: %v, concurrent: %v, duration: %v seconds, qps: %v\n",
		countFinal, concurrent, duration_in_second, countFinal/duration_in_second)
}
