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

package csds

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).Run()
}

func TestCSDS(t *testing.T) {
	// proxy status tests really belong in the istioctl file under pilot, but the experimental proxy status
	// requires custom install flags, so it needs to live here till it is promoted.
	// This test is not yet implemented
	framework.NewTest(t).
		NotImplementedYet("usability.troubleshooting.istioctl.experimental-proxy-status",
			"usability.troubleshooting.envoy.csds.point-in-time",
			"usability.troubleshooting.envoy.csds.streaming")
}
