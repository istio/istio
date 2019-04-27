// Copyright 2019 Istio Authors
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

package trafficshifting

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"testing"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.Main("traffic_shifting", m, istio.SetupOnKube(&ist, nil))
}

func TestTrafficShifting(t *testing.T) {
	// Traffic distribution
	var weights = [5][]int32{
		{0, 100},
		{20, 80},
		{50, 50},
		{33, 33, 34},
		{25, 25, 25, 25},
	}

	for _, v := range weights {
		testTrafficShifting(t, v)
	}
}

func testTrafficShifting(t *testing.T, weight []int32) {
	RunTrafficShiftingTest(t, weight)
}
