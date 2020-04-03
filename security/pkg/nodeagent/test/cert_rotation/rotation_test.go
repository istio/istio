// Copyright 2020 Istio Authors
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

package rotatesds

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/mixer/test/client/env"
	testID "istio.io/istio/mixer/test/client/env"
	sdsTest "istio.io/istio/security/pkg/nodeagent/test"
)

func TestCertRotation(t *testing.T) {
	rotateInterval := 1 * time.Second
	sdsTest.RotateCert(rotateInterval)
	setup := sdsTest.SetupTest(t, testID.SDSCertRotation)
	defer setup.TearDown()

	setup.StartProxy(t)
	start := time.Now()
	for {
		code, _, err := env.HTTPGet(fmt.Sprintf("http://localhost:%d/echo", setup.OutboundListenerPort))
		if err != nil {
			t.Errorf("Failed in request: %v", err)
		}
		if code != 200 {
			t.Errorf("Unexpected status code: %d", code)
		}
		if time.Since(start) > 2*rotateInterval {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
